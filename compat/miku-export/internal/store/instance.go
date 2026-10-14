package store

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
)

// InspectInstance reads the stable identity without migrating a database or
// opening its credential vault. It is safe to use while the control service owns
// the exclusive write/lifecycle lease, including from the root installer.
func InspectInstance(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return "", errors.New("实例数据库不存在或不是普通文件；请先初始化")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	uri := url.URL{Scheme: "file", OmitHost: true, Path: filepath.ToSlash(absolute), RawQuery: "mode=ro"}
	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		return "", err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	var id string
	if err = db.QueryRow("SELECT value FROM metadata WHERE key='instance_id'").Scan(&id); err != nil {
		return "", fmt.Errorf("无法只读取得实例ID；旧数据需在停止服务并备份后升级: %w", err)
	}
	if !regexp.MustCompile(`^[A-Za-z0-9_-]{22}$`).MatchString(id) {
		return "", errors.New("实例ID无效，拒绝自动替换")
	}
	return id, nil
}
