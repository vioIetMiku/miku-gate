package vpnipc

import (
	"mikugate/internal/wirejson"
	"strings"
	"testing"
)

func TestProtocolOperationSeparation(t *testing.T) {
	token := strings.Repeat("a", 32)
	valid := []request{{Version: 1, Operation: "capabilities"}, {Version: 1, Operation: "start", ExitID: "exit_12345678", Profile: "client", IP: "8.8.8.8", PeerName: "vpn.example"}, {Version: 1, Operation: "dial", Token: token, Address: "example.com:443"}, {Version: 1, Operation: "verify", Token: token}, {Version: 1, Operation: "stop", Token: token}}
	for _, r := range valid {
		if !r.valid() {
			t.Fatal("valid operation refused", r.Operation)
		}
	}
	bad := []request{{Version: 1, Operation: "exec"}, {Version: 1, Operation: "start", ExitID: "bad/exit", Profile: "x", IP: "8.8.8.8", PeerName: "vpn.example"}, {Version: 1, Operation: "verify", Token: token, Address: "example.com:443"}, {Version: 1, Operation: "dial", Token: token, Profile: "config", Address: "example.com:443"}, {Version: 1, Operation: "capabilities", Legacy: true}}
	for _, r := range bad {
		if r.valid() {
			t.Fatal("operation fields mixed", r.Operation)
		}
	}
	var decoded request
	for _, raw := range []string{`{"version":1,"operation":"start","operation":"exec"}`, `{"version":1,"operation":"start","command":"sh"}`, `{"version":1,"operation":"dial","path":"/etc/passwd"}`} {
		if wirejson.Decode([]byte(raw), &decoded, MaxRequest) == nil {
			t.Fatal("invalid IPC input accepted")
		}
	}
}
