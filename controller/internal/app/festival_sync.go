package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"dropcheck/controller/internal/command"
	"dropcheck/controller/internal/control"
	"dropcheck/controller/internal/controlpb"
	"google.golang.org/protobuf/encoding/protojson"
)

type festivalSyncOptions struct {
	OutputDir  string
	Limit      string
	MarkSynced bool
}

func syncFestivalRuns(ctx context.Context, state *shellState, opts festivalSyncOptions) error {
	agents, err := state.commandTargets()
	if err != nil {
		return err
	}
	outputDir := opts.OutputDir
	if outputDir == "" {
		outputDir = "festival-sync"
	}
	limit, err := parseFestivalSyncLimit(opts.Limit)
	if err != nil {
		return err
	}
	for _, agent := range agents {
		if err := syncFestivalRunsForAgent(ctx, state, agent, outputDir, limit, opts.MarkSynced, len(agents) > 1); err != nil {
			return err
		}
	}
	return nil
}

func syncFestivalRunsForAgent(ctx context.Context, state *shellState, agent control.AgentInfo, outputDir string, limit uint32, markSynced bool, multiAgent bool) error {
	listLimit := ""
	if limit > 0 {
		listLimit = strconv.FormatUint(uint64(limit), 10)
	}
	listOp, err := command.FestivalListRunsOperation(command.FestivalListOptions{Limit: listLimit})
	if err != nil {
		return err
	}
	listResult, err := runFestivalCommand(ctx, state, agent, listOp)
	if err != nil {
		return err
	}
	runs := listResult.GetFestivalRuns()
	if runs == nil || len(runs.GetRuns()) == 0 {
		fmt.Printf("%s: no unsynced Dropcheck Festival runs\n", agentDisplayName(agent))
		return nil
	}
	dir := outputDir
	if multiAgent {
		dir = filepath.Join(outputDir, safePathComponent(agentDisplayName(agent)))
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for _, summary := range runs.GetRuns() {
		getOp, err := command.FestivalRunOperation(summary.GetRunId(), markSynced)
		if err != nil {
			return err
		}
		result, err := runFestivalCommand(ctx, state, agent, getOp)
		if err != nil {
			return err
		}
		archive := result.GetFestivalRun()
		if archive == nil {
			return fmt.Errorf("%s: run %s returned no archive", agentDisplayName(agent), summary.GetRunId())
		}
		path := filepath.Join(dir, safePathComponent(summary.GetRunId())+".json")
		data, err := protojson.MarshalOptions{
			Multiline:     true,
			Indent:        "  ",
			UseProtoNames: true,
		}.Marshal(archive)
		if err != nil {
			return err
		}
		if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
			return err
		}
		fmt.Printf("%s: synced %s -> %s\n", agentDisplayName(agent), summary.GetRunId(), path)
	}
	return nil
}

func runFestivalCommand(ctx context.Context, state *shellState, agent control.AgentInfo, op command.Operation) (*controlpb.CommandResult, error) {
	cmd, _, err := buildRunCommand(op)
	if err != nil {
		return nil, err
	}
	commandID, err := control.RandomHex(8)
	if err != nil {
		return nil, err
	}
	runCtx, cancel := context.WithTimeout(ctx, timeoutFor(cmd))
	defer cancel()
	return state.server.Run(runCtx, agent.ID, commandID, cmd)
}

func parseFestivalSyncLimit(value string) (uint32, error) {
	if value == "" {
		return 0, nil
	}
	limit, err := strconv.ParseUint(value, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("festival sync limit must be a positive integer: %w", err)
	}
	return uint32(limit), nil
}

var unsafePathComponent = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func safePathComponent(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	return strings.Trim(unsafePathComponent.ReplaceAllString(value, "_"), "_")
}
