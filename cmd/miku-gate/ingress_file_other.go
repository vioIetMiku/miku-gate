//go:build !linux

package main

import "errors"

func writePrivateCandidate(string, []byte) error {
	return errors.New("候选配置创建仅支持 Linux；Windows 控制台不会假装管理 VPS 文件")
}
