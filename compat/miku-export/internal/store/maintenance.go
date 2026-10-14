package store

import (
	"database/sql"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"regexp"

	"mikugate/internal/secretstore"
	"mikugate/internal/security"
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
	if db.QueryRow("PRAGMA user_version").Scan(&result.Schema) != nil || (result.Schema != 6 && result.Schema != 7 && result.Schema != 8 && result.Schema != 9 && result.Schema != 10 && result.Schema != 11 && result.Schema != 12) || db.QueryRow("PRAGMA quick_check").Scan(&integrity) != nil || integrity != "ok" {
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
	backupIDs, e := s.PanelBackupIDs()
	if e != nil {
		return result, e
	}
	for _, id := range backupIDs {
		private, e := s.PanelBackup(id)
		if e != nil {
			return result, secretstore.ErrCiphertext
		}
		clear(private)
		result.EncryptedRecordCount++
	}
	subscriptionIDs, e := s.SubscriptionIDs()
	if e != nil {
		return result, e
	}
	for _, id := range subscriptionIDs {
		if _, e := s.Subscription(id); e != nil {
			return result, secretstore.ErrCiphertext
		}
		result.EncryptedRecordCount++
	}
	fleetHosts, e := s.FleetHosts()
	if e != nil {
		return result, e
	}
	result.EncryptedRecordCount += len(fleetHosts)
	rows, e := db.Query("SELECT substr(key,10) FROM metadata WHERE key LIKE 'proxy-v1:%' LIMIT 101")
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
	if e != nil || len(ids) > 100 {
		return result, secretstore.ErrCiphertext
	}
	for _, id := range ids {
		if _, e = s.ProxyPassword(id); e != nil {
			return result, secretstore.ErrCiphertext
		}
		result.EncryptedRecordCount++
	}
	if result.Schema >= 12 {
		ids, e := s.EntranceIDs()
		if e != nil {
			return result, e
		}
		for _, id := range ids {
			view, _, _, e := s.EntranceRoutes(id)
			if e != nil {
				return result, secretstore.ErrCiphertext
			}
			result.EncryptedRecordCount += 1 + len(view.Landings)
		}
	}
	result.IntegrityValid = true
	result.ProtectedRecordsVerified = true
	return result, nil
}

// VerifyExitProxy reads only the selected logical exit while the live control
// service owns its writer lease. Panel preparation must not migrate or write DB.
func VerifyExitProxy(path, id, username, password string, port int) error {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	info, err := os.Lstat(absolute)
	if err != nil || !info.Mode().IsRegular() {
		return errors.New("出口数据库无效")
	}
	uri := url.URL{Scheme: "file", OmitHost: true, Path: filepath.ToSlash(absolute), RawQuery: "mode=ro"}
	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		return err
	}
	defer db.Close()
	var actualPort int
	var actualUser, hash string
	if db.QueryRow("SELECT port,username,password_hash FROM exits WHERE id=?", id).Scan(&actualPort, &actualUser, &hash) != nil || actualPort != port || actualUser != username || !security.VerifyPassword(hash, password) {
		return errors.New("面板出口与实际逻辑出口凭据/端口不匹配")
	}
	return nil
}
