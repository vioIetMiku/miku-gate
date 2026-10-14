package main

import (
	"encoding/json"
	"strings"
)

// Describe the actual transport and active client flow, never infer Vision
// merely because an inbound uses VLESS or TLS.
func protocolStack(protocol, network, security string, vision, plain bool) string {
	protocol, network, security = strings.ToUpper(protocol), strings.ToUpper(network), strings.ToUpper(security)
	if network == "" || network == "RAW" {
		network = "TCP"
	}
	parts := []string{protocol, network}
	if security != "" && security != "NONE" {
		parts = append(parts, security)
	}
	if protocol == "VLESS" && network == "TCP" && (security == "TLS" || security == "REALITY") && vision {
		flow := "VISION"
		if plain {
			flow += "（可选）"
		}
		parts = append(parts, flow)
	}
	return strings.Join(parts, " + ")
}

func decodePanelJSON(raw json.RawMessage, target any) {
	var encoded string
	if json.Unmarshal(raw, &encoded) == nil {
		raw = json.RawMessage(encoded)
	}
	_ = json.Unmarshal(raw, target)
}

func inboundStackJSON(protocol string, stream, settings json.RawMessage) string {
	var transport struct {
		Network  string `json:"network"`
		Security string `json:"security"`
	}
	var credentials struct {
		Clients []struct {
			Flow   string `json:"flow"`
			Enable *bool  `json:"enable"`
		} `json:"clients"`
	}
	decodePanelJSON(stream, &transport)
	decodePanelJSON(settings, &credentials)
	vision, plain := false, false
	for _, client := range credentials.Clients {
		if client.Enable != nil && !*client.Enable {
			continue
		}
		if strings.HasPrefix(client.Flow, "xtls-rprx-vision") {
			vision = true
		} else {
			plain = true
		}
	}
	return protocolStack(protocol, transport.Network, transport.Security, vision, plain)
}

func (ib *nativeInbound) protocolStack() string {
	vision, plain := false, false
	for _, client := range ib.Clients {
		if !client.Enable {
			continue
		}
		if strings.HasPrefix(client.Flow, "xtls-rprx-vision") {
			vision = true
		} else {
			plain = true
		}
	}
	return protocolStack(ib.Protocol, ib.Network, ib.Security, vision, plain)
}
