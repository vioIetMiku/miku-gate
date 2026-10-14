package portalloc

import (
	"net"
	"strconv"
)

// Check the actual wildcard listener, rather than assuming a database-free
// port is also unused by SSH, a panel or another application. Startup rechecks.
func Free(port int) bool {
	if port < 1024 || port > 65535 {
		return false
	}
	listener, err := net.Listen("tcp4", net.JoinHostPort("0.0.0.0", strconv.Itoa(port)))
	if err != nil {
		return false
	}
	listener.Close()
	return true
}
