//go:build !linux

package certassets

import "errors"

func Discover(string) (Discovery, *Asset, error) {
	return Discovery{}, nil, errors.New("自动发现VPS证书仅支持Linux；不读取Windows上的无关证书")
}
