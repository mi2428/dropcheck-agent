package mcpserver

func successOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"success": booleanSchema(),
		"error":   stringSchema(),
	}, "success")
}

func sessionStartOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"success": booleanSchema(),
		"error":   stringSchema(),
		"session": objectSchema(map[string]any{
			"started":     booleanSchema(),
			"listen_addr": stringSchema(),
			"agent_count": integerSchema(),
			"agents":      arraySchema(agentSchema()),
			"started_at":  stringSchema(),
		}),
	}, "success")
}

func agentsOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"success": booleanSchema(),
		"error":   stringSchema(),
		"agents":  arraySchema(agentSchema()),
	}, "success")
}

func adbDiagnosticsOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"success":     booleanSchema(),
		"error":       stringSchema(),
		"agent":       agentSchema(),
		"diagnostics": objectSchema(map[string]any{}),
	}, "success")
}

func operationOutputSchema() map[string]any {
	return objectSchema(operationOutputProperties(), "success")
}

func commandOutputSchema() map[string]any {
	properties := operationOutputProperties()
	properties["agents"] = arraySchema(agentSchema())
	properties["results"] = arraySchema(operationOutputSchema())
	return objectSchema(properties, "success")
}

func dropcheckRunOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"success":     booleanSchema(),
		"error":       stringSchema(),
		"steps":       arraySchema(operationOutputSchema()),
		"failed_step": stringSchema(),
		"partial":     objectSchema(map[string]any{}),
	}, "success")
}

func operationOutputProperties() map[string]any {
	return map[string]any{
		"success":       booleanSchema(),
		"error":         stringSchema(),
		"agent":         agentSchema(),
		"command_id":    stringSchema(),
		"operation":     stringSchema(),
		"command_label": stringSchema(),
		"status":        stringSchema(),
		"message":       stringSchema(),
		"elapsed_ms":    integerSchema(),
		"result":        objectSchema(map[string]any{}),
	}
}

func agentSchema() map[string]any {
	return objectSchema(map[string]any{
		"number":       integerSchema(),
		"id":           stringSchema(),
		"adb_serial":   stringSchema(),
		"session_id":   stringSchema(),
		"app_version":  stringSchema(),
		"manufacturer": stringSchema(),
		"model":        stringSchema(),
		"device":       stringSchema(),
		"sdk":          integerSchema(),
		"release":      stringSchema(),
		"connected":    stringSchema(),
	})
}

func objectSchema(properties map[string]any, required ...string) map[string]any {
	schema := map[string]any{
		"type":                 "object",
		"properties":           properties,
		"additionalProperties": true,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func arraySchema(items map[string]any) map[string]any {
	return map[string]any{
		"type":  "array",
		"items": items,
	}
}

func stringSchema() map[string]any {
	return map[string]any{"type": "string"}
}

func booleanSchema() map[string]any {
	return map[string]any{"type": "boolean"}
}

func integerSchema() map[string]any {
	return map[string]any{"type": "integer"}
}
