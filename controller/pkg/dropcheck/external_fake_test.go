package dropcheck_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/mi2428/dropcheck-agent/controller/internal/controlpb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func serveExternalFixture(ctx context.Context, dir string) {
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	var wg sync.WaitGroup
	defer wg.Wait()
	previous := ""
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			raw, err := os.ReadFile(filepath.Join(dir, "launch"))
			if err != nil || string(raw) == previous {
				continue
			}
			fields := strings.Fields(string(raw))
			if len(fields) != 3 {
				continue
			}
			previous = string(raw)
			wg.Go(func() { serveExternalSession(ctx, dir, fields) })
		}
	}
}

func serveExternalSession(ctx context.Context, dir string, launch []string) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var wg sync.WaitGroup
	defer wg.Wait()
	connect := func(id string, typed bool) bool {
		conn, err := grpc.NewClient("127.0.0.1:"+launch[0], grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			return false
		}
		stream, err := controlpb.NewDropcheckControlClient(conn).Session(ctx)
		if err != nil {
			_ = conn.Close()
			return false
		}
		capabilities := []string{"wifi.status"}
		if typed {
			capabilities = append(capabilities, "wifi.forget.typed")
		}
		if err := stream.Send(&controlpb.AgentFrame{SessionId: id, Body: &controlpb.AgentFrame_Hello{Hello: &controlpb.AgentHello{Token: launch[1], AdbSerial: launch[2], ControllerAgentId: launch[2], Device: &controlpb.DeviceInfo{Model: "Fake"}, Capabilities: capabilities}}}); err != nil {
			_ = conn.Close()
			return false
		}
		wg.Go(func() {
			defer conn.Close()
			ready := false
			for {
				frame, err := stream.Recv()
				if err != nil {
					return
				}
				if typed && !ready {
					_ = os.WriteFile(filepath.Join(dir, "ready"), []byte("1"), 0o600)
					ready = true
				}
				cmd := frame.GetRunCommand()
				if cmd == nil {
					continue
				}
				if cmd.GetWget() != nil {
					_ = os.WriteFile(filepath.Join(dir, "commands.blocked"), []byte("1"), 0o600)
					continue
				}
				for _, level := range []controlpb.CommandLog_Level{controlpb.CommandLog_LEVEL_DEBUG, controlpb.CommandLog_LEVEL_INFO, controlpb.CommandLog_LEVEL_WARN, controlpb.CommandLog_LEVEL_ERROR} {
					if stream.Send(&controlpb.AgentFrame{CommandId: frame.CommandId, Body: &controlpb.AgentFrame_Log{Log: &controlpb.CommandLog{Level: level, Message: "synthetic event"}}}) != nil {
						return
					}
				}
				if stream.Send(&controlpb.AgentFrame{CommandId: frame.CommandId, Body: &controlpb.AgentFrame_Accepted{Accepted: &controlpb.CommandAccepted{CommandName: "synthetic"}}}) != nil {
					return
				}
				result := &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_OK}
				switch {
				case cmd.GetGetWifiStatus() != nil:
					result.Payload = &controlpb.CommandResult_WifiStatus{WifiStatus: &controlpb.WifiStatus{State: "connected"}}
				case cmd.GetPing() != nil:
					result.Status = controlpb.CommandResult_STATUS_FAILED
				case cmd.GetForgetWifi() != nil:
					result.Payload = &controlpb.CommandResult_WifiOperation{WifiOperation: &controlpb.WifiOperationResult{Ok: true}}
					// Traceroute deliberately has a missing successful payload.
				}
				if stream.Send(&controlpb.AgentFrame{CommandId: frame.CommandId, Body: &controlpb.AgentFrame_Result{Result: result}}) != nil {
					return
				}
			}
		})
		return true
	}
	if !connect("external-1", false) {
		return
	}
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := os.Stat(filepath.Join(dir, "replace")); err == nil {
				_ = connect("external-2", true)
				<-ctx.Done()
				return
			}
		}
	}
}
