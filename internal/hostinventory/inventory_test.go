package hostinventory

import (
	"context"
	"encoding/json"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type info struct{ mode fs.FileMode }

func (i info) Name() string       { return "fixture" }
func (i info) Size() int64        { return 0 }
func (i info) Mode() fs.FileMode  { return i.mode }
func (i info) ModTime() time.Time { return time.Time{} }
func (i info) IsDir() bool        { return i.mode.IsDir() }
func (i info) Sys() any           { return nil }
func fixture(t *testing.T) source {
	t.Helper()
	return source{platform: "linux", arch: "amd64", uid: 65534,
		lstat: func(path string) (fs.FileInfo, error) {
			switch path {
			case "/run/systemd/system":
				return info{fs.ModeDir | 0755}, nil
			case "/dev/net/tun":
				return info{fs.ModeDevice | fs.ModeCharDevice | 0666}, nil
			case "/usr/local/x-ui/x-ui":
				return info{0755}, nil
			case "/etc/x-ui/x-ui.db":
				return nil, fs.ErrPermission
			case "/etc/sing-box/config.json":
				return info{fs.ModeSymlink | 0777}, nil
			default:
				return nil, fs.ErrNotExist
			}
		},
		read: func(path string, limit int) ([]byte, error) {
			if path != "/etc/os-release" || limit != 16384 {
				t.Fatalf("unexpected content access: %s", path)
			}
			return []byte("ID=ubuntu\nVERSION_ID=\"24.04\"\nSECRET=never_return\n"), nil
		},
		lookPath: func(name string) (string, error) {
			if name == "openvpn" {
				return "", fs.ErrNotExist
			}
			return filepath.Join(t.TempDir(), name), nil
		},
		service: func(ctx context.Context, unit string) ([]byte, error) {
			state, active, pid := "not-found", "inactive", "0"
			if unit == "x-ui.service" {
				state, active, pid = "loaded", "active", "123"
			}
			return []byte("Id=" + unit + "\nLoadState=" + state + "\nActiveState=" + active + "\nMainPID=" + pid + "\nEnvironment=SECRET=never_return\nExecStart=/secret\n"), nil
		},
	}
}
func TestInventoryIsEvidenceNotReuseOrInstallation(t *testing.T) {
	r := inspect(context.Background(), fixture(t))
	if !r.ReadOnly || !r.Supported || r.Privileged || r.Distribution != "ubuntu" || r.DistributionVersion != "24.04" || r.TUN != "present" {
		t.Fatalf("unexpected report: %+v", r)
	}
	if len(r.Components) != 5 || r.Components[0].State != "detected" || r.Components[0].Services[0].MainPID != 123 {
		t.Fatal("missing panel evidence")
	}
	if r.Components[1].State != "not_detected" || r.Components[2].State != "unknown" {
		t.Fatal("symlink is not an absent installation")
	}
	for _, c := range r.Components {
		if c.ReuseReady || c.Version != "unknown" {
			t.Fatal("unproven reuse or version")
		}
	}
	if r.Dependencies[0].State != "not_found" || len(r.Warnings) < 4 {
		t.Fatal("missing dependency/limitations")
	}
	raw, _ := json.Marshal(r)
	for _, secret := range []string{"never_return", "Environment", "ExecStart", "/secret"} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("unexpected metadata leak: %s", secret)
		}
	}
}
func TestUnsupportedDoesNotInspectLocalFilesOrExecute(t *testing.T) {
	s := fixture(t)
	s.platform = "windows"
	s.lstat = func(string) (fs.FileInfo, error) { t.Fatal("unsupported read"); return nil, nil }
	s.read = func(string, int) ([]byte, error) { t.Fatal("unsupported content read"); return nil, nil }
	s.service = func(context.Context, string) ([]byte, error) { t.Fatal("unsupported execution"); return nil, nil }
	r := inspect(context.Background(), s)
	if r.Supported || len(r.Components) != 0 || len(r.Warnings) == 0 {
		t.Fatal("unsupported platform misrepresented")
	}
}
func TestCanceledAndUnreadableAreNotAbsence(t *testing.T) {
	s := fixture(t)
	s.lstat = func(string) (fs.FileInfo, error) { return nil, fs.ErrPermission }
	s.service = func(context.Context, string) ([]byte, error) { t.Fatal("canceled query executed"); return nil, nil }
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := inspect(ctx, s)
	for _, c := range r.Components {
		if c.State != "unknown" {
			t.Fatal("permission failures treated as absence")
		}
	}
	if r.Systemd != "unreadable" || !strings.Contains(strings.Join(r.Warnings, ""), "取消") {
		t.Fatal("missing canceled state")
	}
}
func TestMetadataParsersOnlyExposeEnumeratedFields(t *testing.T) {
	for _, raw := range []string{"Id=xray.service\nId=xray.service\nLoadState=loaded", "Id=other.service\nLoadState=loaded", strings.Repeat("x", 16385)} {
		if got := parseService("xray.service", []byte(raw)); got.State != "unavailable" || got.MainPID != 0 {
			t.Fatal("malformed service accepted")
		}
	}
	got := parseService("xray.service", []byte("Id=xray.service\nLoadState=secret\nActiveState=secret\nMainPID=-99"))
	if got.State != "unavailable" || got.Active != "unknown" || got.MainPID != 0 {
		t.Fatal("arbitrary values echoed")
	}
	id, version := parseOSRelease([]byte("ID=$(touch /tmp/unsafe)\nVERSION_ID=\"<secret>\"\n"))
	if id != "unknown" || version != "unknown" {
		t.Fatal("unsafe metadata accepted")
	}
}
