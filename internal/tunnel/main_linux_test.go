//go:build linux

package tunnel

import (
	"fmt"
	"os"
	"testing"
)

// The same fixed helper entry points as the release CLI, not alternate worker
// implementations. Only the outer lab fixture/certificate source is test-owned.
func TestMain(m *testing.M) {
	if len(os.Args) == 2 {
		var err error
		handled := true
		switch os.Args[1] {
		case "_vpn-supervisor":
			err = RunSupervisor()
		case "_vpn-worker":
			err = RunWorker()
		case "_vpn-openvpn":
			err = RunOpenVPN()
		default:
			handled = false
		}
		if handled {
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			os.Exit(0)
		}
	}
	os.Exit(m.Run())
}
