//go:build !linux

package hostinventory

import "context"

func processSockets(context.Context, int, string) (string, []Listener) {
	return "unavailable", []Listener{}
}
func processChildren(context.Context, int, []string) (string, []Process) {
	return "unavailable", []Process{}
}
