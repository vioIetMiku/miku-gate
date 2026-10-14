//go:build linux

package hostinventory

import (
	"context"
	"errors"
	"io"
	"os"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

func procDirectory(pid int, suffix string, limit int) ([]string, error) {
	if pid <= 0 || pid >= 1<<30 || (suffix != "fd" && suffix != "task") {
		return nil, errors.New("invalid proc directory")
	}
	fd, err := unix.Open("/proc/"+strconv.Itoa(pid)+"/"+suffix, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), "proc metadata")
	defer f.Close()
	names, err := f.Readdirnames(limit + 1)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	if len(names) > limit {
		return nil, errors.New("proc directory bound")
	}
	for _, name := range names {
		if !numericName(name) {
			return nil, errors.New("invalid proc entry")
		}
	}
	return names, nil
}
func procRead(pid int, suffix string, limit int) ([]byte, error) {
	if pid <= 0 || pid >= 1<<30 {
		return nil, errors.New("invalid pid")
	}
	allowed := suffix == "net/tcp" || suffix == "net/tcp6"
	if parts := strings.Split(suffix, "/"); len(parts) == 3 && parts[0] == "task" && numericName(parts[1]) && parts[2] == "children" {
		allowed = true
	}
	if !allowed || limit < 1 || limit > 1<<20 {
		return nil, errors.New("invalid proc metadata")
	}
	f, err := openRegular("/proc/"+strconv.Itoa(pid)+"/"+suffix, int64(limit))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, int64(limit)+1))
	if err != nil || len(raw) > limit {
		return nil, errors.New("proc content bound")
	}
	return raw, nil
}
func procLink(pid int, suffix string) (string, error) {
	if pid <= 0 || pid >= 1<<30 {
		return "", errors.New("invalid pid")
	}
	if suffix != "ns/net" {
		parts := strings.Split(suffix, "/")
		if len(parts) != 2 || parts[0] != "fd" || !numericName(parts[1]) {
			return "", errors.New("invalid proc link")
		}
	}
	return os.Readlink("/proc/" + strconv.Itoa(pid) + "/" + suffix)
}
func procChildren(ctx context.Context, pid int) ([]int, error) {
	threads, err := procDirectory(pid, "task", 128)
	if err != nil {
		return nil, err
	}
	out := []int{}
	seen := map[int]bool{}
	for _, thread := range threads {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		raw, err := procRead(pid, "task/"+thread+"/children", 4096)
		if err != nil {
			return nil, err
		}
		for _, entry := range strings.Fields(string(raw)) {
			child, err := strconv.Atoi(entry)
			if err != nil || !numericName(entry) || child <= 0 || child == pid {
				return nil, errors.New("invalid child metadata")
			}
			if !seen[child] {
				out = append(out, child)
				seen[child] = true
			}
			if len(out) > 16 {
				return nil, errors.New("child metadata bound")
			}
		}
	}
	return out, nil
}
func nativeSocketSource() socketSource {
	return socketSource{start: readProcessStart, identity: processIdentity,
		namespace: func(pid int) (string, error) { return procLink(pid, "ns/net") },
		read:      procRead, fds: func(pid int) ([]string, error) { return procDirectory(pid, "fd", 4096) },
		link: func(pid int, fd string) (string, error) { return procLink(pid, "fd/"+fd) }, children: procChildren}
}
func processSockets(ctx context.Context, pid int, path string) (string, []Listener) {
	return inspectSockets(ctx, pid, path, nativeSocketSource())
}
func processChildren(ctx context.Context, pid int, paths []string) (string, []Process) {
	return inspectChildren(ctx, pid, paths, nativeSocketSource())
}
