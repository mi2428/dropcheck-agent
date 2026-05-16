package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	dropcmd "dropcheck/controller/internal/command"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const resourceMIMEJSON = "application/json"

func registerResources(server *mcp.Server, backend Backend) {
	server.AddResource(&mcp.Resource{
		Name:        "dropcheck-session",
		Title:       "Dropcheck Session",
		Description: "Current controller session state without starting a new Android session.",
		MIMEType:    resourceMIMEJSON,
		URI:         "dropcheck://session",
	}, readSessionResource(backend))

	server.AddResource(&mcp.Resource{
		Name:        "dropcheck-agents",
		Title:       "Dropcheck Agents",
		Description: "Connected Android dropcheck agents.",
		MIMEType:    resourceMIMEJSON,
		URI:         "dropcheck://agents",
	}, readAgentsResource(backend))

	server.AddResourceTemplate(&mcp.ResourceTemplate{
		Name:        "dropcheck-standalone-config",
		Title:       "Standalone Config",
		Description: "Persisted standalone configuration for one target. Use default when exactly one agent is connected.",
		MIMEType:    resourceMIMEJSON,
		URITemplate: "dropcheck://standalone/config/{target}",
	}, readStandaloneOperationResource(backend, "config"))

	server.AddResourceTemplate(&mcp.ResourceTemplate{
		Name:        "dropcheck-standalone-status",
		Title:       "Standalone Status",
		Description: "Standalone runtime status and archive counters for one target. Use default when exactly one agent is connected.",
		MIMEType:    resourceMIMEJSON,
		URITemplate: "dropcheck://standalone/status/{target}",
	}, readStandaloneOperationResource(backend, "status"))

	server.AddResourceTemplate(&mcp.ResourceTemplate{
		Name:        "dropcheck-standalone-runs",
		Title:       "Standalone Runs",
		Description: "Stored standalone run summaries for one target. Use default when exactly one agent is connected.",
		MIMEType:    resourceMIMEJSON,
		URITemplate: "dropcheck://standalone/runs/{target}",
	}, readStandaloneOperationResource(backend, "runs"))

	server.AddResourceTemplate(&mcp.ResourceTemplate{
		Name:        "dropcheck-standalone-run",
		Title:       "Standalone Run Archive",
		Description: "One stored standalone run archive for one target.",
		MIMEType:    resourceMIMEJSON,
		URITemplate: "dropcheck://standalone/run/{target}/{run_id}",
	}, readStandaloneRunResource(backend))
}

func readSessionResource(backend Backend) mcp.ResourceHandler {
	return func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		info, err := backend.Info(ctx)
		if err != nil {
			return nil, err
		}
		return jsonResource(req.Params.URI, map[string]any{"success": true, "session": info})
	}
}

func readAgentsResource(backend Backend) mcp.ResourceHandler {
	return func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		agents, err := backend.Agents(ctx)
		if err != nil {
			return nil, err
		}
		return jsonResource(req.Params.URI, map[string]any{"success": true, "agents": agents})
	}
}

func readStandaloneOperationResource(backend Backend, kind string) mcp.ResourceHandler {
	return func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		target, err := standaloneResourceTarget(req.Params.URI, kind)
		if err != nil {
			return nil, err
		}
		var op dropcmd.Operation
		switch kind {
		case "config":
			op = dropcmd.StandaloneConfigOperation()
		case "status":
			op = dropcmd.StandaloneStatusOperation()
		case "runs":
			op, err = dropcmd.StandaloneListRunsOperation(dropcmd.StandaloneListOptions{})
			if err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("unsupported standalone resource kind %q", kind)
		}
		return operationResource(ctx, backend, req.Params.URI, target, op)
	}
}

func readStandaloneRunResource(backend Backend) mcp.ResourceHandler {
	return func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		target, runID, err := standaloneRunResourceParts(req.Params.URI)
		if err != nil {
			return nil, err
		}
		op, err := dropcmd.StandaloneRunOperation(runID, false)
		if err != nil {
			return nil, err
		}
		return operationResource(ctx, backend, req.Params.URI, target, op)
	}
}

func operationResource(ctx context.Context, backend Backend, uri string, target string, op dropcmd.Operation) (*mcp.ReadResourceResult, error) {
	exec, err := backend.Run(ctx, target, op)
	if err != nil {
		return nil, err
	}
	out, _, _, err := executionMap(exec)
	if err != nil {
		return nil, err
	}
	return jsonResource(uri, out)
}

func standaloneResourceTarget(rawURI string, kind string) (string, error) {
	parts, err := resourcePathParts(rawURI, "standalone")
	if err != nil {
		return "", err
	}
	if len(parts) != 2 || parts[0] != kind {
		return "", mcp.ResourceNotFoundError(rawURI)
	}
	return resourceTarget(parts[1]), nil
}

func standaloneRunResourceParts(rawURI string) (string, string, error) {
	parts, err := resourcePathParts(rawURI, "standalone")
	if err != nil {
		return "", "", err
	}
	if len(parts) != 3 || parts[0] != "run" {
		return "", "", mcp.ResourceNotFoundError(rawURI)
	}
	if parts[2] == "" {
		return "", "", fmt.Errorf("standalone run id is required")
	}
	return resourceTarget(parts[1]), parts[2], nil
}

func resourcePathParts(rawURI string, host string) ([]string, error) {
	parsed, err := url.Parse(rawURI)
	if err != nil {
		return nil, err
	}
	if parsed.Scheme != "dropcheck" || parsed.Host != host {
		return nil, mcp.ResourceNotFoundError(rawURI)
	}
	path := strings.Trim(parsed.EscapedPath(), "/")
	if path == "" {
		return nil, mcp.ResourceNotFoundError(rawURI)
	}
	escapedParts := strings.Split(path, "/")
	parts := make([]string, 0, len(escapedParts))
	for _, part := range escapedParts {
		decoded, err := url.PathUnescape(part)
		if err != nil {
			return nil, err
		}
		parts = append(parts, decoded)
	}
	return parts, nil
}

func resourceTarget(value string) string {
	switch value {
	case "", "default", "_":
		return ""
	default:
		return value
	}
}

func jsonResource(uri string, value any) (*mcp.ReadResourceResult, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return &mcp.ReadResourceResult{
		Contents: []*mcp.ResourceContents{{
			URI:      uri,
			MIMEType: resourceMIMEJSON,
			Text:     string(data),
		}},
	}, nil
}
