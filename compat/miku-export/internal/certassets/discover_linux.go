//go:build linux

package certassets

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

type candidate struct {
	provider, cert, key, root string
	le                        bool
}

func candidates(host string) []candidate {
	return []candidate{
		{"letsencrypt", "/etc/letsencrypt/live/" + host + "/fullchain.pem", "/etc/letsencrypt/live/" + host + "/privkey.pem", "/etc/letsencrypt/archive/" + host, true},
		{"acme-rsa", "/root/.acme.sh/" + host + "/fullchain.cer", "/root/.acme.sh/" + host + "/" + host + ".key", "/root/.acme.sh/" + host, false},
		{"acme-ecc", "/root/.acme.sh/" + host + "_ecc/fullchain.cer", "/root/.acme.sh/" + host + "_ecc/" + host + ".key", "/root/.acme.sh/" + host + "_ecc", false},
	}
}

// rootFile opens the resolved name with dirfds and no symlinks, then verifies
// actual descriptors. A checked pathname is not used for a later privileged read.
func rootFile(path string, private bool) ([]byte, error) {
	fd, e := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if e != nil {
		return nil, e
	}
	defer func() { unix.Close(fd) }()
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	for i, name := range parts {
		if name == "" || name == "." || name == ".." {
			return nil, os.ErrPermission
		}
		flags := unix.O_RDONLY | unix.O_NOFOLLOW | unix.O_CLOEXEC | unix.O_NONBLOCK
		if i < len(parts)-1 {
			flags |= unix.O_DIRECTORY
		}
		next, e := unix.Openat(fd, name, flags, 0)
		if e != nil {
			return nil, e
		}
		unix.Close(fd)
		fd = next
		var st unix.Stat_t
		if unix.Fstat(fd, &st) != nil || st.Uid != 0 || st.Mode&0022 != 0 {
			return nil, os.ErrPermission
		}
		if i == len(parts)-1 {
			if st.Mode&unix.S_IFMT != unix.S_IFREG || st.Nlink != 1 || st.Size > MaxPEM || private && st.Mode&0077 != 0 {
				return nil, os.ErrPermission
			}
		}
	}
	f := os.NewFile(uintptr(fd), path)
	fd = -1
	defer f.Close()
	raw, e := io.ReadAll(io.LimitReader(f, MaxPEM+1))
	if e != nil || len(raw) > MaxPEM {
		return nil, os.ErrPermission
	}
	return raw, nil
}
func readCandidate(c candidate, path string, private bool) ([]byte, error) {
	if c.le {
		// Standard live symlinks only: every source directory and link must be
		// root-owned, and the resolved destination remains in the same archive name.
		dir := filepath.Dir(path)
		for _, p := range []string{"/etc", "/etc/letsencrypt", "/etc/letsencrypt/live", dir, path} {
			if _, e := os.Lstat(p); e != nil {
				return nil, e
			}
			var s unix.Stat_t
			if unix.Lstat(p, &s) != nil || s.Uid != 0 || s.Mode&0022 != 0 && s.Mode&unix.S_IFMT != unix.S_IFLNK {
				return nil, os.ErrPermission
			}
		}
		resolved, e := filepath.EvalSymlinks(path)
		if e != nil {
			return nil, e
		}
		if !strings.HasPrefix(resolved, c.root+string(filepath.Separator)) {
			return nil, os.ErrPermission
		}
		return rootFile(resolved, private)
	}
	return rootFile(path, private)
}
func Discover(host string) (Discovery, *Asset, error) {
	host, e := Hostname(host)
	if e != nil {
		return Discovery{}, nil, e
	}
	report := Discovery{Hostname: host, Findings: []Finding{}}
	var best *Asset
	for _, c := range candidates(host) {
		finding := Finding{Provider: c.provider, State: "unreadable"}
		cert, e1 := readCandidate(c, c.cert, false)
		key, e2 := readCandidate(c, c.key, true)
		if errors.Is(e1, os.ErrNotExist) && errors.Is(e2, os.ErrNotExist) {
			finding.State = "not_found"
		} else if e1 == nil && e2 == nil {
			asset, e := Validate(host, cert, key)
			if e != nil {
				finding.State = "unverified"
			} else {
				finding.State = "verified"
				summary := asset.Summary()
				finding.Certificate = &summary
				if best == nil || asset.Summary().NotAfter.After(best.Summary().NotAfter) {
					best = asset
					report.SelectedProvider = c.provider
				}
			}
		}
		report.Findings = append(report.Findings, finding)
	}
	return report, best, nil
}
