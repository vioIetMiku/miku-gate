package main

import "strings"

// The manager owns liveness and connection deadlines. Imported inactivity or
// ping-exit directives must not fight its recovery window. Keep inline PKI
// byte-for-byte: certificate/key contents are not OpenVPN options.
func managedVPNProfile(raw string) string {
	var out []string
	block := ""
	for _, line := range strings.Split(raw, "\n") {
		trimmed := strings.TrimSpace(line)
		if block != "" {
			out = append(out, line)
			if trimmed == "</"+block+">" {
				block = ""
			}
			continue
		}
		if strings.HasPrefix(trimmed, "<") && !strings.HasPrefix(trimmed, "</") {
			block = strings.Trim(trimmed, "<>")
		}
		fields := strings.Fields(trimmed)
		if len(fields) > 0 {
			option := strings.ToLower(strings.TrimLeft(strings.Trim(strings.ReplaceAll(fields[0], "\\", ""), "\"'"), "-"))
			switch option {
			case "keepalive", "ping", "ping-restart", "ping-exit", "inactive", "connect-retry", "connect-retry-max", "connect-timeout", "hand-window":
				continue
			}
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

func (t *Tunnel) openVPNArgs(cfgPath, authPath, logPath string) []string {
	timeout := "20"
	if t.quickCandidate.Load() {
		timeout = "8"
	}
	return []string{"netns", "exec", t.nsName(), "openvpn",
		"--pull-filter", "ignore", "ping",
		"--pull-filter", "ignore", "inactive",
		"--config", cfgPath,
		"--auth-user-pass", authPath, "--auth-nocache",
		"--dev", "tun0", "--persist-tun", "--persist-key",
		"--ping", "10", "--ping-restart", "45",
		"--connect-retry-max", "2", "--connect-timeout", timeout,
		"--connect-retry", "1", "8",
		"--data-ciphers", "AES-128-CBC:AES-256-GCM:AES-128-GCM:CHACHA20-POLY1305",
		"--verb", "3", "--log", logPath,
	}
}
