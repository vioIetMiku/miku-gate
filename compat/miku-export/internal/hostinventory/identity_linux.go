//go:build linux

package hostinventory

import (
	"debug/buildinfo"
	"errors"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

// Fixed paths only; never execute a discovered binary to obtain its version.
func binaryModule(path string) string {
	for _, spec := range specifications {
		for _, evidence := range spec.files {
			if evidence.Path != path {
				continue
			}
			if evidence.Kind == "embedded_core" || spec.id == "xray" {
				return "github.com/xtls/xray-core"
			}
			if evidence.Kind != "binary" {
				return ""
			}
			switch spec.id {
			case "x-ui":
				return "github.com/mhsanaei/3x-ui/v3"
			case "sing-box":
				return "github.com/sagernet/sing-box"
			case "caddy":
				return "github.com/caddyserver/caddy/v2"
			}
		}
	}
	return ""
}

func openRegular(path string, limit int64) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > limit {
		f.Close()
		return nil, errors.New("invalid metadata file")
	}
	return f, nil
}

type boundedReaderAt struct {
	f         *os.File
	remaining int
}

func (r *boundedReaderAt) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 || len(p) > 1<<20 || len(p) > r.remaining {
		return 0, errors.New("build metadata budget")
	}
	r.remaining -= len(p)
	return r.f.ReadAt(p, off)
}

var releaseVersion = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z]+([.-][0-9A-Za-z]+)*)?$`)

func reportedVersion(module, version, expected string) string {
	// Main module version is self-reported; devel/ldflags-only builds stay unknown.
	matches := module == expected || (expected == "github.com/mhsanaei/3x-ui/v3" && module == "github.com/mhsanaei/3x-ui")
	if expected == "" || !matches || len(version) > 80 || !releaseVersion.MatchString(version) {
		return "unknown"
	}
	return version
}

func binaryVersion(path string) string {
	expected := binaryModule(path)
	if expected == "" {
		return "unknown"
	}
	f, err := openRegular(path, 128<<20)
	if err != nil {
		return "unknown"
	}
	defer f.Close()
	info, err := buildinfo.Read(&boundedReaderAt{f: f, remaining: 2 << 20})
	if err != nil || info.Main.Replace != nil {
		return "unknown"
	}
	return reportedVersion(info.Main.Path, info.Main.Version, expected)
}

// Parse only the start-time field; process names/arguments are never returned.
func processStart(raw []byte, pid int) (string, error) {
	end := strings.LastIndexByte(string(raw), ')')
	prefix, _, ok := strings.Cut(string(raw), " (")
	if !ok || prefix != strconv.Itoa(pid) || end < len(prefix)+2 {
		return "", errors.New("invalid process metadata")
	}
	fields := strings.Fields(string(raw[end+1:]))
	if len(fields) < 20 {
		return "", errors.New("short process metadata")
	}
	start, err := strconv.ParseUint(fields[19], 10, 64)
	if err != nil || start == 0 {
		return "", errors.New("invalid process start")
	}
	return strconv.FormatUint(start, 10), nil
}
func readProcessStart(pid int) (string, error) {
	f, err := openRegular("/proc/"+strconv.Itoa(pid)+"/stat", 8192)
	if err != nil {
		return "", err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, 8193))
	if err != nil || len(raw) > 8192 {
		return "", errors.New("process metadata bound")
	}
	return processStart(raw, pid)
}
func processIdentity(pid int, paths []string) (string, string) {
	if pid <= 0 || pid >= 1<<30 {
		return "unknown", ""
	}
	before, err := readProcessStart(pid)
	if err != nil {
		return "unreadable", ""
	}
	exePath := "/proc/" + strconv.Itoa(pid) + "/exe"
	exe, err := os.Stat(exePath)
	if err != nil {
		return "unreadable", ""
	}
	matched := ""
	for _, path := range paths {
		// Do not accept arbitrary paths from callers or follow final symlinks.
		allowed := false
		for _, spec := range specifications {
			for _, e := range spec.files {
				if e.Path == path && (e.Kind == "binary" || e.Kind == "embedded_core") {
					allowed = true
				}
			}
		}
		if !allowed {
			continue
		}
		f, err := openRegular(path, 128<<20)
		if err != nil {
			continue
		}
		info, err := f.Stat()
		f.Close()
		if err == nil && os.SameFile(exe, info) {
			matched = path
			break
		}
	}
	after, err := readProcessStart(pid)
	current, statErr := os.Stat(exePath)
	if err != nil || statErr != nil || before != after || !os.SameFile(exe, current) {
		return "changed", ""
	}
	if matched == "" {
		return "unrecognized", ""
	}
	return "matched", matched
}
