package hostinventory

import (
	"context"
	"encoding/hex"
	"errors"
	"net"
	"sort"
	"strconv"
	"strings"
)

type Listener struct {
	Address string `json:"address"`
	Port    int    `json:"port"`
	Family  string `json:"family"`
}
type Process struct {
	PID           int        `json:"pid"`
	ProcessState  string     `json:"process_state"`
	Executable    string     `json:"executable,omitempty"`
	ListenerState string     `json:"listener_state"`
	Listeners     []Listener `json:"listeners"`
}
type kernelSocket struct {
	listener Listener
	inode    string
}
type socketSource struct {
	start     func(int) (string, error)
	identity  func(int, []string) (string, string)
	namespace func(int) (string, error)
	read      func(int, string, int) ([]byte, error)
	fds       func(int) ([]string, error)
	link      func(int, string) (string, error)
	children  func(context.Context, int) ([]int, error)
}

func kernelID(raw, prefix string) (string, bool) {
	if !strings.HasPrefix(raw, prefix+":[") || !strings.HasSuffix(raw, "]") {
		return "", false
	}
	value := strings.TrimSuffix(strings.TrimPrefix(raw, prefix+":["), "]")
	n, err := strconv.ParseUint(value, 10, 64)
	return value, err == nil && n > 0 && strconv.FormatUint(n, 10) == value
}
func numericName(raw string) bool {
	n, err := strconv.ParseUint(raw, 10, 31)
	return err == nil && strconv.FormatUint(n, 10) == raw
}
func parseOwnedSockets(raw []byte, ipv6 bool) ([]kernelSocket, error) {
	if len(raw) > 1<<20 {
		return nil, errors.New("socket metadata bound")
	}
	lines := strings.Split(string(raw), "\n")
	if len(lines) == 0 || !strings.Contains(lines[0], "local_address") {
		return nil, errors.New("socket metadata header")
	}
	out := []kernelSocket{}
	rows := 0
	for _, line := range lines[1:] {
		if strings.TrimSpace(line) == "" {
			continue
		}
		rows++
		if rows > 4096 {
			return nil, errors.New("socket row bound")
		}
		fields := strings.Fields(line)
		if len(fields) < 10 {
			return nil, errors.New("socket row fields")
		}
		if fields[3] != "0A" {
			continue
		}
		rawAddr, rawPort, ok := strings.Cut(fields[1], ":")
		expected := 4
		family := "tcp4"
		if ipv6 {
			expected = 16
			family = "tcp6"
		}
		addr, err := hex.DecodeString(rawAddr)
		if !ok || err != nil || len(addr) != expected || len(rawPort) != 4 {
			return nil, errors.New("socket address")
		}
		port, err := strconv.ParseUint(rawPort, 16, 16)
		if err != nil || port == 0 {
			return nil, errors.New("socket port")
		}
		inode, err := strconv.ParseUint(fields[9], 10, 64)
		if err != nil || inode == 0 || strconv.FormatUint(inode, 10) != fields[9] {
			return nil, errors.New("socket inode")
		}
		for i := 0; i < len(addr); i += 4 {
			addr[i], addr[i+3] = addr[i+3], addr[i]
			addr[i+1], addr[i+2] = addr[i+2], addr[i+1]
		}
		out = append(out, kernelSocket{Listener{net.IP(addr).String(), int(port), family}, fields[9]})
	}
	return out, nil
}
func inspectSockets(ctx context.Context, pid int, expected string, src socketSource) (string, []Listener) {
	empty := []Listener{}
	if pid <= 0 || pid >= 1<<30 || ctx.Err() != nil {
		return "unavailable", empty
	}
	state, path := src.identity(pid, []string{expected})
	if state != "matched" || path != expected {
		return "unavailable", empty
	}
	before, err := src.start(pid)
	if err != nil {
		return "unavailable", empty
	}
	namespace, err := src.namespace(pid)
	if err != nil {
		return "unavailable", empty
	}
	if _, ok := kernelID(namespace, "net"); !ok {
		return "unavailable", empty
	}
	fds, err := src.fds(pid)
	if err != nil || len(fds) > 4096 {
		return "unavailable", empty
	}
	owned := map[string][]string{}
	complete := true
	for _, fd := range fds {
		if ctx.Err() != nil {
			return "unavailable", empty
		}
		if !numericName(fd) {
			return "unavailable", empty
		}
		target, err := src.link(pid, fd)
		if err != nil {
			complete = false
			continue
		}
		if inode, ok := kernelID(target, "socket"); ok {
			owned[inode] = append(owned[inode], fd)
		}
	}
	all := []kernelSocket{}
	for _, family := range []string{"tcp", "tcp6"} {
		if ctx.Err() != nil {
			return "unavailable", empty
		}
		raw, err := src.read(pid, "net/"+family, 1<<20)
		if err != nil {
			complete = false
			continue
		}
		sockets, err := parseOwnedSockets(raw, family == "tcp6")
		if err != nil {
			complete = false
			continue
		}
		all = append(all, sockets...)
	}
	out := []Listener{}
	seen := map[Listener]bool{}
	for _, s := range all {
		if ctx.Err() != nil {
			return "unavailable", empty
		}
		confirmed := false
		for _, fd := range owned[s.inode] {
			target, err := src.link(pid, fd)
			inode, ok := kernelID(target, "socket")
			if err == nil && ok && inode == s.inode {
				confirmed = true
				break
			}
			complete = false
		}
		if !confirmed || seen[s.listener] {
			continue
		}
		seen[s.listener] = true
		if len(out) == 64 {
			complete = false
			continue
		}
		out = append(out, s.listener)
	}
	after, err := src.start(pid)
	current, nsErr := src.namespace(pid)
	state, path = src.identity(pid, []string{expected})
	if err != nil || nsErr != nil || before != after || namespace != current || state != "matched" || path != expected {
		return "changed", empty
	}
	if ctx.Err() != nil {
		return "unavailable", empty
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Family != b.Family {
			return a.Family < b.Family
		}
		if a.Address != b.Address {
			return a.Address < b.Address
		}
		return a.Port < b.Port
	})
	if !complete {
		return "partial", out
	}
	if len(out) == 0 {
		return "not_observed", out
	}
	return "verified_snapshot", out
}
func inspectChildren(ctx context.Context, pid int, paths []string, src socketSource) (string, []Process) {
	empty := []Process{}
	if ctx.Err() != nil {
		return "unavailable", empty
	}
	before, err := src.start(pid)
	if err != nil {
		return "unavailable", empty
	}
	state, parent := src.identity(pid, paths)
	if state != "matched" {
		return "unavailable", empty
	}
	children, err := src.children(ctx, pid)
	if err != nil || len(children) > 16 {
		return "unavailable", empty
	}
	out := []Process{}
	for _, child := range children {
		if ctx.Err() != nil {
			return "unavailable", empty
		}
		state, exe := src.identity(child, paths)
		p := Process{PID: child, ProcessState: state, Executable: exe, ListenerState: "unknown", Listeners: []Listener{}}
		if state == "matched" {
			p.ListenerState, p.Listeners = inspectSockets(ctx, child, exe, src)
		}
		out = append(out, p)
	}
	after, err := src.start(pid)
	state, exe := src.identity(pid, paths)
	remaining, childErr := src.children(ctx, pid)
	if err != nil || childErr != nil || before != after || state != "matched" || exe != parent {
		return "changed", empty
	}
	live := map[int]bool{}
	for _, id := range remaining {
		live[id] = true
	}
	if len(remaining) != len(children) {
		return "changed", empty
	}
	for _, p := range out {
		if !live[p.PID] {
			return "changed", empty
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PID < out[j].PID })
	return "observed", out
}
