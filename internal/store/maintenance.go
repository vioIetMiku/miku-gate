package store

import (
	"database/sql"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"regexp"

	"mikugate/internal/secretstore"
)

// MaintenanceInspection contains only nonsecret assertions. It never creates a
// database/key, migrates schemas, acquires a writer lease, or starts networking.
type MaintenanceInspection struct {
	Instance                 string `json:"instance"`
	Schema                   int    `json:"schema"`
	EncryptedRecordCount     int    `json:"encrypted_record_count"`
	IntegrityValid           bool   `json:"integrity_valid"`
	ProtectedRecordsVerified bool   `json:"protected_records_verified"`
}

func InspectMaintenance(path string) (MaintenanceInspection, error) {
	var result MaintenanceInspection
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return result, errors.New("维护检查数据库不存在或类型无效")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return result, err
	}
	uri := url.URL{Scheme: "file", OmitHost: true, Path: filepath.ToSlash(absolute), RawQuery: "mode=ro"}
	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		return result, err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	s := &Store{db: db, path: absolute}
	var integrity string
	if db.QueryRow("PRAGMA user_version").Scan(&result.Schema) != nil || result.Schema != 6 || db.QueryRow("PRAGMA quick_check").Scan(&integrity) != nil || integrity != "ok" {
		return result, errors.New("维护检查schema或数据库完整性无效")
	}
	if db.QueryRow("SELECT value FROM metadata WHERE key='instance_id'").Scan(&result.Instance) != nil || !regexp.MustCompile(`^[A-Za-z0-9_-]{22}$`).MatchString(result.Instance) {
		return result, errors.New("维护检查实例身份无效")
	}
	// Close each row cursor before using the existing authenticated decoders;
	// the store uses one connection and this also bounds maintenance work.
	for _, table := range []string{"upstreams", "vpn_configs", "managed_ingress"} {
		rows, e := db.Query("SELECT exit_id FROM " + table + " LIMIT 4097")
		if e != nil {
			return result, e
		}
		ids := []string{}
		for rows.Next() {
			var id string
			if e = rows.Scan(&id); e != nil {
				rows.Close()
				return result, e
			}
			ids = append(ids, id)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return result, e
		}
		if len(ids) > 4096 {
			return result, errors.New("维护检查超过当前行预算")
		}
		for _, id := range ids {
			switch table {
			case "upstreams":
				_, _, _, e = s.Upstream(id)
			case "vpn_configs":
				_, _, e = s.VPN(id)
			case "managed_ingress":
				_, _, _, e = s.Managed(id)
			}
			if e != nil {
				return result, secretstore.ErrCiphertext
			}
		}
		result.EncryptedRecordCount += len(ids)
	}
	result.IntegrityValid = true
	result.ProtectedRecordsVerified = true
	return result, nil
}
