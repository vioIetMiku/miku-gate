//go:build !linux

package ingress

import "errors"

func ReadSource(Source) ([]byte, error) {
	return nil, errors.New("本机核心配置检测仅支持 Linux")
}
