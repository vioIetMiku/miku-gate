package main

import (
	"testing"
)

func TestClientLandingsAcrossCores(t *testing.T) {
	ib := &nativeInbound{ID: 1, Port: 30000, Protocol: "vless", Enable: true, BoundTo: "jp", Clients: []nativeClient{{Email: "jp-user", ID: newUUID(), Enable: true, BoundTo: "jp"}, {Email: "us-user", ID: newUUID(), Enable: true, BoundTo: "us"}}}
	tunnels := []*Tunnel{{Port: 35000, Status: "up", Node: Node{HostName: "jp"}}, {Port: 35001, Status: "failed", Node: Node{HostName: "us"}}}
	xray := buildXrayConfig([]*nativeInbound{ib}, tunnels)
	rules := xray["routing"].(map[string]any)["rules"].([]any)
	if len(rules) != 3 || rules[0].(map[string]any)["outboundTag"] != "miku-jp" || rules[1].(map[string]any)["outboundTag"] != "block" {
		t.Fatal("Xray per-user routing or failed-tunnel isolation missing", rules)
	}
	sing, err := buildSingBoxConfig([]*nativeInbound{ib}, tunnels)
	if err != nil {
		t.Fatal(err)
	}
	rules = sing["route"].(map[string]any)["rules"].([]any)
	if len(sing["inbounds"].([]any)) != 1 || len(rules) != 3 || rules[0].(map[string]any)["outbound"] != "miku-jp" || rules[1].(map[string]any)["action"] != "reject" {
		t.Fatal("sing-box per-user routing or failed-tunnel isolation missing", rules)
	}
}

func TestSingBoxRejectsXHTTP(t *testing.T) {
	_, err := buildSingBoxConfig([]*nativeInbound{{Enable: true, Protocol: "vless", Network: "xhttp"}}, nil)
	if err == nil {
		t.Fatal("unsupported transport accepted")
	}
}
