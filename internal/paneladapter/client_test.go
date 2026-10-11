package paneladapter

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestLocalPersistentClientCSRFAndReadback(t *testing.T) {
	template := map[string]any{"inbounds": []any{}, "outbounds": []any{map[string]any{"tag": "direct", "protocol": "freedom"}}, "routing": map[string]any{"rules": []any{}}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/fixture/")
		w.Header().Set("Content-Type", "application/json")
		send := func(obj any) { json.NewEncoder(w).Encode(map[string]any{"success": true, "obj": obj}) }
		if path == "csrf-token" {
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "fixture", Path: "/"})
			send("fixture-csrf-at-least16")
			return
		}
		if r.Method == "POST" && r.Header.Get("X-CSRF-Token") != "fixture-csrf-at-least16" {
			t.Error("CSRF missing")
			w.WriteHeader(403)
			return
		}
		switch path {
		case "login":
			r.ParseForm()
			if r.Form.Get("password") != "private-login-fixture" {
				t.Error("login payload")
			}
			send(nil)
		case "panel/api/xray/":
			send(map[string]any{"xraySetting": template, "outboundTestUrl": "https://fixture.example/"})
		case "panel/api/server/getConfigJson":
			send(template)
		case "panel/api/xray/update":
			r.ParseForm()
			if json.Unmarshal([]byte(r.Form.Get("xraySetting")), &template) != nil {
				t.Error("save persistent JSON")
			}
			send(nil)
		case "panel/api/server/restartXrayService":
			send(nil)
		default:
			t.Error("unexpected panel path", path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	_, port, _ := net.SplitHostPort(server.Listener.Addr().String())
	n, _ := strconv.Atoi(port)
	runtime, e := New(Endpoint{Port: n, Base: "/fixture/", Mode: "current"})
	if e != nil {
		t.Fatal(e)
	}
	if e = runtime.Login(t.Context(), "admin", "private-login-fixture", ""); e != nil {
		t.Fatal(e)
	}
	before, e := runtime.Read(t.Context())
	if e != nil {
		t.Fatal(e)
	}
	candidate := before
	candidate.Template = []byte(`{"inbounds":[],"outbounds":[],"routing":{"rules":[]}}`)
	if e = runtime.Save(t.Context(), candidate); e != nil {
		t.Fatal(e)
	}
	after, e := runtime.Read(t.Context())
	if e != nil || Hash(after.Template) == Hash(before.Template) {
		t.Fatal("persistent API did not change", e)
	}
	if e = runtime.Save(context.Background(), before); e != nil {
		t.Fatal(e)
	}
	restored, e := runtime.Read(t.Context())
	if e != nil || !restored.Same(before) {
		t.Fatal("old template not restored", e)
	}
}
func TestPanelStrictJSONEndpointAndRedirect(t *testing.T) {
	for _, endpoint := range []Endpoint{{Port: 80, Base: "/", Mode: "current"}, {Port: 2053, Base: "/../", Mode: "current"}, {Port: 2053, Base: "https://evil.example/", Mode: "current"}, {Port: 2053, Base: "/", Mode: "auto"}} {
		if _, e := New(endpoint); e == nil {
			t.Fatal("unsafe panel endpoint")
		}
	}
	if _, e := Canonical([]byte(`{"inbounds":[],"inbounds":[]}`)); e == nil {
		t.Fatal("ambiguous panel JSON")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "http://192.0.2.10/", 302) }))
	defer server.Close()
	_, port, _ := net.SplitHostPort(server.Listener.Addr().String())
	n, _ := strconv.Atoi(port)
	client, _ := New(Endpoint{n, "/", "current"})
	if client.Login(t.Context(), "user", "secret", "") == nil {
		t.Fatal("panel login redirect allowed")
	}
}
