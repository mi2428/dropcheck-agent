package mcpserver

import (
	"encoding/json"
	"fmt"
	"strings"

	"dropcheck/controller/internal/controlpb"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

func toolResult(text string, structured map[string]any, isError bool) (*mcp.CallToolResult, map[string]any, error) {
	return &mcp.CallToolResult{
		IsError: isError,
		Content: []mcp.Content{
			&mcp.TextContent{Text: text},
		},
	}, structured, nil
}

func toolError(message string, fields map[string]any) (*mcp.CallToolResult, map[string]any, error) {
	if fields == nil {
		fields = map[string]any{}
	}
	fields["success"] = false
	fields["error"] = message
	return toolResult(message, fields, true)
}

func executionToolResult(exec Execution) (*mcp.CallToolResult, map[string]any, error) {
	out, ok, text, err := executionMap(exec)
	if err != nil {
		return toolError(err.Error(), map[string]any{"operation": exec.Operation})
	}
	return toolResult(text, out, !ok)
}

func executionMap(exec Execution) (map[string]any, bool, string, error) {
	if exec.Result == nil {
		return nil, false, "", fmt.Errorf("operation %s returned no command result", exec.Operation)
	}
	result, err := protoMap(exec.Result)
	if err != nil {
		return nil, false, "", err
	}
	status := statusName(exec.Result.GetStatus())
	ok := exec.Result.GetStatus() == controlpb.CommandResult_STATUS_OK
	text := fmt.Sprintf("%s %s", exec.Operation, status)
	if exec.Result.GetMessage() != "" {
		text += ": " + exec.Result.GetMessage()
	}
	return map[string]any{
		"success":       ok,
		"agent":         exec.Agent,
		"command_id":    exec.CommandID,
		"operation":     exec.Operation,
		"command_label": exec.CommandLabel,
		"status":        status,
		"message":       exec.Result.GetMessage(),
		"elapsed_ms":    exec.Result.GetElapsedMs(),
		"result":        result,
	}, ok, text, nil
}

func executionsToolResult(execs []Execution) (*mcp.CallToolResult, map[string]any, error) {
	results := make([]map[string]any, 0, len(execs))
	success := true
	var failed []string
	for _, exec := range execs {
		result, ok, _, err := executionMap(exec)
		if err != nil {
			return toolError(err.Error(), map[string]any{"operation": exec.Operation})
		}
		results = append(results, result)
		if !ok {
			success = false
			failed = append(failed, exec.Operation)
		}
	}
	out := map[string]any{
		"success": success,
		"results": results,
	}
	text := fmt.Sprintf("%d operation(s) completed", len(execs))
	if !success {
		text = "failed operation(s): " + strings.Join(failed, ", ")
	}
	return toolResult(text, out, !success)
}

func protoMap(message proto.Message) (map[string]any, error) {
	data, err := protojson.MarshalOptions{
		UseProtoNames: true,
	}.Marshal(message)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func statusName(status controlpb.CommandResult_Status) string {
	name := strings.ToLower(strings.TrimPrefix(status.String(), "STATUS_"))
	if name == "" || name == "unspecified" {
		return "unspecified"
	}
	return name
}

func annotations(readOnly bool, destructive *bool, idempotent bool) *mcp.ToolAnnotations {
	closedWorld := false
	return &mcp.ToolAnnotations{
		ReadOnlyHint:    readOnly,
		DestructiveHint: destructive,
		IdempotentHint:  idempotent,
		OpenWorldHint:   &closedWorld,
	}
}
