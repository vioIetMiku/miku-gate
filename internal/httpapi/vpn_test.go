package httpapi

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"mikugate/internal/domain"
	"mikugate/internal/gateway"
	"mikugate/internal/security"
	"mikugate/internal/store"
	"mikugate/internal/vpngate"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestVPNSelectionAPIRequiresConsentAndSeparateExecutor(t *testing.T) {
	data, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer data.Close()
	hash, _ := security.HashPassword("test-admin")
	if data.InitAccount("admin", hash) != nil {
		t.Fatal("account")
	}
	data.CreateSession("admin", "vpn-api-session", "token", time.Now().Add(time.Hour))
	exit, _, err := data.CreateExit(domain.ExitInput{Name: "VPN binding", Country: "JP"})
	if err != nil {
		t.Fatal(err)
	}
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	now := time.Now()
	ca := &x509.Certificate{SerialNumber: big.NewInt(123), Subject: pkix.Name{CommonName: "API fixture root"}, IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, ca, ca, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	profile := "client\ndev tun\nremote 8.8.8.8 1194\n<ca>\n" + string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})) + "</ca>\n"
	node := domain.Node{ID: "vpn-api-fixture", Hostname: "vpn.example", IP: "8.8.8.8", Source: vpngate.Source, FetchedAt: now, Config: profile}
	if data.ReplaceNodes(t.Context(), []domain.Node{node}) != nil {
		t.Fatal("cache")
	}
	manager, err := gateway.New(t.Context(), data, gateway.Options{Factory: gateway.WithVPN(data, nil)})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close(context.Background())
	handler, err := NewWithOptions(data, nil, Options{Gateway: manager})
	if err != nil {
		t.Fatal(err)
	}
	request := func(method, path string, body any, session, csrf bool) *httptest.ResponseRecorder {
		t.Helper()
		raw, _ := json.Marshal(body)
		r := httptest.NewRequest(method, "http://localhost/api"+path, bytes.NewReader(raw))
		r.Header.Set("Content-Type", "application/json")
		if session {
			r.AddCookie(&http.Cookie{Name: "miku_session", Value: "vpn-api-session"})
		}
		if csrf {
			r.Header.Set("X-CSRF-Token", "token")
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	prefix := "/exits/" + exit.ID
	if w := request("GET", "/vpn-executor", nil, false, false); w.Code != 401 {
		t.Fatal("anonymous daemon metadata exposed")
	}
	input := domain.VPNInput{NodeID: node.ID, PeerName: "vpn.example", Confirm: true}
	if w := request("PUT", prefix+"/vpn", input, true, false); w.Code != 403 {
		t.Fatal("missing CSRF accepted")
	}
	input.Confirm = false
	if w := request("PUT", prefix+"/vpn", input, true, true); w.Code != 400 {
		t.Fatal("missing consent accepted", w.Code, w.Body.String())
	}
	input.Confirm = true
	if w := request("PUT", prefix+"/vpn", input, true, true); w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	w := request("GET", prefix+"/vpn", nil, true, false)
	if w.Code != 200 || strings.Contains(w.Body.String(), "BEGIN CERTIFICATE") || strings.Contains(w.Body.String(), "\"profile\"") {
		t.Fatal("private profile echoed", w.Code)
	}
	var view domain.VPNConfig
	if json.Unmarshal(w.Body.Bytes(), &view) != nil || !view.Configured || view.NodeID != node.ID {
		t.Fatal("bad binding view")
	}
	if w = request("POST", prefix+"/start", nil, true, true); w.Code != 409 || !strings.Contains(w.Body.String(), "vpn_executor_disabled") {
		t.Fatal("unconfigured root service was bypassed", w.Code, w.Body.String())
	}
	if w = request("GET", "/overview", nil, true, false); !strings.Contains(w.Body.String(), `"active_count":0`) {
		t.Fatal("binding fabricated running state")
	}
	if w = request("GET", "/vpn-executor", nil, true, false); !strings.Contains(w.Body.String(), `"configured":false`) {
		t.Fatal("executor inferred from stored config")
	}
	if w = request("DELETE", prefix+"/vpn", nil, true, true); w.Code != 204 {
		t.Fatal(w.Code)
	}
}
