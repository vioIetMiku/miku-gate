//go:build !linux

package main

import "errors"

func writeManagedDirectory(string, map[string][]byte) error {
	return errors.New("受管入口bundle生成仅支持Linux，本机不会代管远端VPS")
}
