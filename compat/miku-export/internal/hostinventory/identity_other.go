//go:build !linux

package hostinventory

func binaryVersion(string) string                    { return "unknown" }
func processIdentity(int, []string) (string, string) { return "unknown", "" }
