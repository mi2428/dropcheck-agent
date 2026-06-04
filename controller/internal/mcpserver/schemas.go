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

func wifiStatusOutputSchema() map[string]any {
	return operationOutputSchemaWithResult(commandResultSchema("wifi_status", wifiStatusSchema()))
}

func ipStatusOutputSchema() map[string]any {
	return operationOutputSchemaWithResult(commandResultSchema("ip_status", ipStatusSchema()))
}

func pingOutputSchema() map[string]any {
	return operationOutputSchemaWithResult(commandResultSchema("ping", objectSchema(map[string]any{
		"host":                stringSchema(),
		"count":               integerSchema(),
		"transmitted":         integerSchema(),
		"received":            integerSchema(),
		"packet_loss_percent": numberSchema(),
		"min_ms":              numberSchema(),
		"avg_ms":              numberSchema(),
		"max_ms":              numberSchema(),
		"elapsed_ms":          int64Schema(),
		"interface_name":      stringSchema(),
		"size_bytes":          integerSchema(),
		"output":              stringSchema(),
	})))
}

func dnsOutputSchema() map[string]any {
	return operationOutputSchemaWithResult(commandResultSchema("resolve_dns", objectSchema(map[string]any{
		"name":       stringSchema(),
		"answers":    arraySchema(objectSchema(map[string]any{"type": stringSchema(), "address": stringSchema()})),
		"elapsed_ms": int64Schema(),
		"error":      stringSchema(),
	})))
}

func httpOutputSchema() map[string]any {
	return operationOutputSchemaWithResult(commandResultSchema("http_check", objectSchema(map[string]any{
		"url":             stringSchema(),
		"status":          integerSchema(),
		"expected_status": integerSchema(),
		"matched":         booleanSchema(),
		"elapsed_ms":      int64Schema(),
		"error":           stringSchema(),
	})))
}

func standaloneRunOutputSchema() map[string]any {
	return operationOutputSchemaWithResult(commandResultSchema("standalone_run", objectSchema(map[string]any{
		"summary": objectSchema(map[string]any{
			"run_id":            stringSchema(),
			"festa_name":        stringSchema(),
			"config_hash":       stringSchema(),
			"started_unix_ms":   int64Schema(),
			"finished_unix_ms":  int64Schema(),
			"status":            stringSchema(),
			"wifi_group_count":  integerSchema(),
			"step_count":        integerSchema(),
			"failed_step_count": integerSchema(),
			"synced":            booleanSchema(),
			"message":           stringSchema(),
		}),
		"steps": arraySchema(objectSchema(map[string]any{
			"wifi_group_index": integerSchema(),
			"wifi_group_name":  stringSchema(),
			"step_index":       integerSchema(),
			"step_name":        stringSchema(),
			"attempt":          integerSchema(),
			"started_unix_ms":  int64Schema(),
			"finished_unix_ms": int64Schema(),
			"error":            stringSchema(),
		})),
	})))
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

func operationOutputSchemaWithResult(result map[string]any) map[string]any {
	properties := operationOutputProperties()
	properties["result"] = result
	return objectSchema(properties, "success")
}

func commandResultSchema(payloadName string, payload map[string]any) map[string]any {
	return objectSchema(map[string]any{
		"status":     stringSchema(),
		"message":    stringSchema(),
		"elapsed_ms": int64Schema(),
		payloadName:  payload,
	})
}

func wifiStatusSchema() map[string]any {
	return objectSchema(map[string]any{
		"enabled":            booleanSchema(),
		"state":              stringSchema(),
		"active_network":     stringSchema(),
		"wifi_network_count": integerSchema(),
		"connection":         wifiConnectionSchema(),
		"ip_status":          ipStatusSchema(),
		"permissions":        arraySchema(stringSchema()),
	})
}

func wifiConnectionSchema() map[string]any {
	return objectSchema(map[string]any{
		"ssid":                 stringSchema(),
		"bssid":                stringSchema(),
		"rssi_dbm":             integerSchema(),
		"network_id":           integerSchema(),
		"supplicant_state":     stringSchema(),
		"frequency_mhz":        integerSchema(),
		"link_speed_mbps":      integerSchema(),
		"tx_link_speed_mbps":   integerSchema(),
		"rx_link_speed_mbps":   integerSchema(),
		"wifi_standard":        stringSchema(),
		"security_type":        stringSchema(),
		"ipv4_address":         stringSchema(),
		"mac_address":          stringSchema(),
		"channel_width":        stringSchema(),
		"ap_mld_mac_address":   stringSchema(),
		"ap_mlo_link_id":       integerSchema(),
		"affiliated_mlo_links": arraySchema(mloLinkSchema()),
		"associated_mlo_links": arraySchema(mloLinkSchema()),
	})
}

func diagnosticFieldSchema() map[string]any {
	return objectSchema(map[string]any{
		"key":   stringSchema(),
		"value": stringSchema(),
	})
}

func ipStatusSchema() map[string]any {
	return objectSchema(map[string]any{
		"network_id":              stringSchema(),
		"transports":              arraySchema(stringSchema()),
		"validated":               booleanSchema(),
		"internet":                booleanSchema(),
		"interface_name":          stringSchema(),
		"mtu":                     integerSchema(),
		"addresses":               arraySchema(stringSchema()),
		"dns_servers":             arraySchema(stringSchema()),
		"dhcp_server":             stringSchema(),
		"routes":                  arraySchema(stringSchema()),
		"wifi":                    wifiConnectionSchema(),
		"capabilities":            arraySchema(stringSchema()),
		"downstream_kbps":         integerSchema(),
		"upstream_kbps":           integerSchema(),
		"signal_strength":         integerSchema(),
		"private_dns_active":      booleanSchema(),
		"private_dns_server_name": stringSchema(),
		"ipv6_ra":                 arraySchema(diagnosticFieldSchema()),
	})
}

func mloLinkSchema() map[string]any {
	return objectSchema(map[string]any{
		"link_id":                          integerSchema(),
		"state":                            stringSchema(),
		"band":                             stringSchema(),
		"channel":                          integerSchema(),
		"rssi_dbm":                         integerSchema(),
		"tx_link_speed_mbps":               integerSchema(),
		"rx_link_speed_mbps":               integerSchema(),
		"ap_mac_address":                   stringSchema(),
		"sta_mac_address":                  stringSchema(),
		"max_supported_tx_link_speed_mbps": integerSchema(),
		"max_supported_rx_link_speed_mbps": integerSchema(),
	})
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

func int64Schema() map[string]any {
	return map[string]any{"type": []string{"integer", "string"}}
}

func numberSchema() map[string]any {
	return map[string]any{"type": "number"}
}
