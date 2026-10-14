//go:build !linux

package ingress

import "errors"

func ReadSource(Source) ([]byte, error) {
	return nil, errors.New("本机核心配置检测仅支持 Linux")
}

func ReadPanelSource(Source, uint32) ([]byte, error) {
	return nil, errors.New("明确面板所有者的读取仅支持 Linux")
}

func ObserveListeners(*Report) {}
