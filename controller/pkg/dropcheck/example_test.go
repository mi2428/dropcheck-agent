package dropcheck_test

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/mi2428/dropcheck-agent/controller/pkg/dropcheck"
)

// This hardware example is compiled, not run. Return through the deferred Close
// instead of calling log.Fatal/os.Exit, which would skip cleanup.
func ExampleOpen() {
	inspect := func(ctx context.Context) (err error) {
		client, err := dropcheck.Open(ctx, dropcheck.Options{Serial: "DEVICE_SERIAL"})
		if err != nil {
			return err
		}
		defer func() { err = errors.Join(err, client.Close()) }()
		agents := client.Agents()
		if len(agents) == 0 {
			return errors.New("no connected agent")
		}
		probe, err := dropcheck.Ping(dropcheck.PingOptions{Host: "192.0.2.1", Count: 1, Timeout: 5 * time.Second})
		if err != nil {
			return err
		}
		probe, err = probe.WithNetwork(dropcheck.NetworkSelector{SSID: "123", NetworkID: 42})
		if err != nil {
			return err
		}
		result, err := client.Run(ctx, agents[0], probe)
		if err != nil {
			return err
		}
		if result.Raw == nil || result.Raw.GetStatus() != dropcheck.StatusOK {
			return errors.New("agent acquisition failed")
		}
		for _, finding := range result.Findings {
			if !finding.Passed || finding.Missing {
				return errors.New("acquisition evidence failed validation")
			}
		}
		ping := result.Raw.GetPing()
		if ping == nil || ping.Received == nil {
			return errors.New("received packet observation is missing")
		}
		fmt.Println(*ping.Received)
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := inspect(ctx); err != nil {
		// Includes a Close failure even when acquisition succeeded.
		fmt.Println("inspection or cleanup failed:", err)
	}
}

func ExampleOperation_WithNetwork() {
	op, err := dropcheck.IPStatus().WithNetwork(dropcheck.NetworkSelector{SSID: "123", NetworkID: 42})
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(op.Name())
	// Output: ip.status
}

func ExampleResult() {
	zero := uint32(0)
	result := dropcheck.Result{Raw: &dropcheck.CommandResult{
		Status:  dropcheck.StatusOK,
		Payload: &dropcheck.PingPayload{Ping: &dropcheck.PingResult{Received: &zero}},
	}}
	if raw := result.Raw; raw != nil && raw.GetStatus() == dropcheck.StatusOK {
		if ping := raw.GetPing(); ping != nil && ping.Received != nil {
			fmt.Println("received:", *ping.Received) // Known zero, not missing data.
		}
	}
	// Output: received: 0
}
