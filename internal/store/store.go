package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"

	"mikugate/internal/domain"
	"mikugate/internal/secretstore"
	"mikugate/internal/security"
)

type Store struct {
	db      *sql.DB
	path    string
	vaultMu sync.Mutex
	vault   *secretstore.Vault
}

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = f.Close(); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec("PRAGMA foreign_keys=ON; PRAGMA busy_timeout=5000; PRAGMA journal_mode=WAL;"); err != nil {
		db.Close()
		return nil, err
	}
	s := &Store{db: db, path: path}
	if err = s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	var protected int
	if err := db.QueryRow("SELECT (SELECT count(*) FROM upstreams)+(SELECT count(*) FROM vpn_configs)").Scan(&protected); err != nil {
		db.Close()
		return nil, err
	}
	if protected > 0 {
		if _, err := s.secretVault(); err != nil {
			db.Close()
			return nil, err
		}
	}
	return s, nil
}
func (s *Store) Close() error { return s.db.Close() }
func (s *Store) migrate() error {
	var version int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version > 5 {
		return fmt.Errorf("数据库版本 %d 高于程序支持的版本 5，拒绝降级打开", version)
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.Exec(`
 CREATE TABLE IF NOT EXISTS schema_version(version INTEGER PRIMARY KEY);
 INSERT OR IGNORE INTO schema_version VALUES(1);
 INSERT OR IGNORE INTO schema_version VALUES(2);
 INSERT OR IGNORE INTO schema_version VALUES(3);
 INSERT OR IGNORE INTO schema_version VALUES(4);
 INSERT OR IGNORE INTO schema_version VALUES(5);
 CREATE TABLE IF NOT EXISTS metadata(key TEXT PRIMARY KEY,value TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS accounts(username TEXT PRIMARY KEY,password_hash TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS sessions(token_hash TEXT PRIMARY KEY,username TEXT NOT NULL REFERENCES accounts(username),csrf TEXT NOT NULL,expires INTEGER NOT NULL);
 CREATE TABLE IF NOT EXISTS exits(id TEXT PRIMARY KEY,name TEXT NOT NULL,country TEXT NOT NULL,policy TEXT NOT NULL,port INTEGER NOT NULL UNIQUE,listen TEXT NOT NULL,username TEXT NOT NULL,password_hash TEXT NOT NULL,status TEXT NOT NULL,reason TEXT NOT NULL,created_at TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS nodes(id TEXT PRIMARY KEY,hostname TEXT NOT NULL,ip TEXT NOT NULL,country TEXT NOT NULL,country_name TEXT NOT NULL,ping INTEGER NOT NULL,speed INTEGER NOT NULL,sessions INTEGER NOT NULL,source TEXT NOT NULL,fetched_at TEXT NOT NULL,config TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS events(id INTEGER PRIMARY KEY AUTOINCREMENT,level TEXT NOT NULL,kind TEXT NOT NULL,message TEXT NOT NULL,exit_id TEXT NOT NULL,created_at TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS upstreams(exit_id TEXT PRIMARY KEY REFERENCES exits(id) ON DELETE CASCADE,kind TEXT NOT NULL,sealed BLOB NOT NULL,updated_at TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS vpn_configs(exit_id TEXT PRIMARY KEY REFERENCES exits(id) ON DELETE CASCADE,sealed BLOB NOT NULL);
 PRAGMA user_version=5;
 `)
	if err != nil {
		return err
	}
	var instanceID string
	err = tx.QueryRow("SELECT value FROM metadata WHERE key='instance_id'").Scan(&instanceID)
	if errors.Is(err, sql.ErrNoRows) {
		if version >= 2 {
			return errors.New("数据库缺少实例 ID；拒绝自动生成新资源身份")
		}
		instanceID, err = security.Token(16)
		if err != nil {
			return err
		}
		if _, err = tx.Exec("INSERT INTO metadata(key,value) VALUES('instance_id',?)", instanceID); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	if !regexp.MustCompile(`^[A-Za-z0-9_-]{22}$`).MatchString(instanceID) {
		return errors.New("数据库中的实例 ID 无效；拒绝自动替换资源身份")
	}
	return tx.Commit()
}
func (s *Store) InstanceID() (string, error) {
	var id string
	err := s.db.QueryRow("SELECT value FROM metadata WHERE key='instance_id'").Scan(&id)
	return id, err
}
func (s *Store) Initialized() (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM accounts`).Scan(&n)
	return n > 0, err
}
func (s *Store) InitAccount(username, hash string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var n int
	if err = tx.QueryRow(`SELECT COUNT(*) FROM accounts`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return domain.ErrConflict
	}
	if _, err = tx.Exec(`INSERT INTO accounts VALUES(?,?)`, username, hash); err != nil {
		return err
	}
	if err = event(tx, "info", "account.initialized", "管理员已在本机初始化", ""); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) PasswordHash(username string) (string, error) {
	var hash string
	err := s.db.QueryRow(`SELECT password_hash FROM accounts WHERE username=?`, username).Scan(&hash)
	return hash, err
}
func (s *Store) CreateSession(username, token, csrf string, expiry time.Time) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`DELETE FROM sessions WHERE expires<=?`, time.Now().Unix()); err != nil {
		return err
	}
	if _, err = tx.Exec(`INSERT INTO sessions VALUES(?,?,?,?)`, security.Digest(token), username, csrf, expiry.Unix()); err != nil {
		return err
	}
	if _, err = tx.Exec(`DELETE FROM sessions WHERE token_hash NOT IN (SELECT token_hash FROM sessions ORDER BY expires DESC LIMIT 32)`); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) Session(token string) (domain.Session, error) {
	var a domain.Session
	var expiry int64
	err := s.db.QueryRow(`SELECT username,csrf,expires FROM sessions WHERE token_hash=? AND expires>?`, security.Digest(token), time.Now().Unix()).Scan(&a.Username, &a.CSRF, &expiry)
	a.ExpiresAt = time.Unix(expiry, 0)
	return a, err
}
func (s *Store) DeleteSession(token string) error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE token_hash=?`, security.Digest(token))
	return err
}
func event(tx *sql.Tx, level, kind, message, id string) error {
	if _, err := tx.Exec(`INSERT INTO events(level,kind,message,exit_id,created_at) VALUES(?,?,?,?,?)`, level, kind, message, id, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return err
	}
	_, err := tx.Exec(`DELETE FROM events WHERE id NOT IN (SELECT id FROM events ORDER BY id DESC LIMIT 500)`)
	return err
}
func (s *Store) AddEvent(level, kind, message, id string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = event(tx, level, kind, message, id); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) CreateExit(in domain.ExitInput) (domain.Exit, string, error) {
	if err := in.Validate(); err != nil {
		return domain.Exit{}, "", err
	}
	id, err := security.Token(12)
	if err != nil {
		return domain.Exit{}, "", err
	}
	pass, err := security.Token(18)
	if err != nil {
		return domain.Exit{}, "", err
	}
	hash, err := security.HashPassword(pass)
	if err != nil {
		return domain.Exit{}, "", err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return domain.Exit{}, "", err
	}
	defer tx.Rollback()
	var count int
	if err = tx.QueryRow(`SELECT COUNT(*) FROM exits`).Scan(&count); err != nil {
		return domain.Exit{}, "", err
	}
	if count >= 100 {
		return domain.Exit{}, "", fmt.Errorf("%w: 出口配置最多 100 条", domain.ErrConflict)
	}
	port := in.Port
	if port == 0 {
		for port = 20000; port <= 29999; port++ {
			var used int
			if err = tx.QueryRow(`SELECT COUNT(*) FROM exits WHERE port=?`, port).Scan(&used); err != nil {
				return domain.Exit{}, "", err
			}
			if used == 0 {
				break
			}
		}
		if port > 29999 {
			return domain.Exit{}, "", domain.ErrConflict
		}
	}
	e := domain.Exit{ID: id, Name: in.Name, Country: in.Country, Policy: in.Policy, Port: port, Listen: "127.0.0.1", Username: "mg_" + id[:8], Status: "stopped", Reason: "配置已保存；未建立 VPN 或监听代理，隔离状态需单独查询。", CreatedAt: time.Now().UTC()}
	_, err = tx.Exec(`INSERT INTO exits VALUES(?,?,?,?,?,?,?,?,?,?,?)`, e.ID, e.Name, e.Country, e.Policy, e.Port, e.Listen, e.Username, hash, e.Status, e.Reason, e.CreatedAt.Format(time.RFC3339Nano))
	if err != nil {
		return domain.Exit{}, "", constraint(err)
	}
	if err = event(tx, "info", "exit.created", "已创建出口配置："+e.Name, e.ID); err != nil {
		return domain.Exit{}, "", err
	}
	if err = tx.Commit(); err != nil {
		return domain.Exit{}, "", err
	}
	return e, pass, nil
}
func constraint(err error) error {
	if strings.Contains(err.Error(), "UNIQUE constraint failed") {
		return fmt.Errorf("%w: 端口已被其他出口配置占用", domain.ErrConflict)
	}
	return err
}
func (s *Store) ListExits() ([]domain.Exit, error) {
	rows, err := s.db.Query(`SELECT e.id,e.name,e.country,e.policy,e.port,e.listen,e.username,e.status,e.reason,e.created_at,CASE WHEN v.exit_id IS NOT NULL THEN 'openvpn' ELSE COALESCE(u.kind,'') END FROM exits e LEFT JOIN upstreams u ON u.exit_id=e.id LEFT JOIN vpn_configs v ON v.exit_id=e.id ORDER BY e.created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Exit{}
	for rows.Next() {
		var e domain.Exit
		var created string
		if err = rows.Scan(&e.ID, &e.Name, &e.Country, &e.Policy, &e.Port, &e.Listen, &e.Username, &e.Status, &e.Reason, &created, &e.UpstreamKind); err != nil {
			return nil, err
		}
		e.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		out = append(out, e)
	}
	return out, rows.Err()
}
func (s *Store) UpdateExit(id string, in domain.ExitInput) error {
	if err := in.Validate(); err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	r, err := tx.Exec(`UPDATE exits SET name=?,country=?,policy=? WHERE id=?`, in.Name, in.Country, in.Policy, id)
	if err != nil {
		return err
	}
	n, _ := r.RowsAffected()
	if n == 0 {
		return domain.ErrNotFound
	}
	if err = event(tx, "info", "exit.updated", "已更新出口配置："+in.Name, id); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) SetStatus(id, status, reason string) error {
	if status != "stopped" && status != "blocked" {
		return errors.New("执行器不可用，拒绝设置活动状态")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	r, err := tx.Exec(`UPDATE exits SET status=?,reason=? WHERE id=?`, status, reason, id)
	if err != nil {
		return err
	}
	n, _ := r.RowsAffected()
	if n == 0 {
		return domain.ErrNotFound
	}
	level := "info"
	if status == "blocked" {
		level = "warning"
	}
	if err = event(tx, level, "exit."+status, reason, id); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) DeleteExit(id string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	r, err := tx.Exec(`DELETE FROM exits WHERE id=?`, id)
	if err != nil {
		return err
	}
	n, _ := r.RowsAffected()
	if n == 0 {
		return domain.ErrNotFound
	}
	if err = event(tx, "info", "exit.deleted", "已删除出口配置", id); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) RotateCredentials(id string) (string, error) {
	pass, err := security.Token(18)
	if err != nil {
		return "", err
	}
	hash, err := security.HashPassword(pass)
	if err != nil {
		return "", err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	r, err := tx.Exec(`UPDATE exits SET password_hash=? WHERE id=?`, hash, id)
	if err != nil {
		return "", err
	}
	n, _ := r.RowsAffected()
	if n == 0 {
		return "", domain.ErrNotFound
	}
	if err = event(tx, "info", "exit.credentials_rotated", "代理凭据已重置；旧凭据失效", id); err != nil {
		return "", err
	}
	return pass, tx.Commit()
}
func (s *Store) Events() ([]domain.Event, error) {
	rows, err := s.db.Query(`SELECT id,level,kind,message,exit_id,created_at FROM events ORDER BY id DESC LIMIT 100`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Event{}
	for rows.Next() {
		var e domain.Event
		var created string
		if err = rows.Scan(&e.ID, &e.Level, &e.Kind, &e.Message, &e.ExitID, &created); err != nil {
			return nil, err
		}
		e.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		out = append(out, e)
	}
	return out, rows.Err()
}
func (s *Store) ReplaceNodes(ctx context.Context, nodes []domain.Node) error {
	if len(nodes) == 0 {
		return errors.New("拒绝用空结果覆盖节点缓存")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `DELETE FROM nodes`); err != nil {
		return err
	}
	for _, n := range nodes {
		_, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO nodes VALUES(?,?,?,?,?,?,?,?,?,?,?)`, n.ID, n.Hostname, n.IP, n.Country, n.CountryName, n.AdvertisedPing, n.AdvertisedSpeed, n.Sessions, n.Source, n.FetchedAt.Format(time.RFC3339Nano), n.Config)
		if err != nil {
			return err
		}
	}
	if err = event(tx, "info", "nodes.refreshed", fmt.Sprintf("节点列表已同步：%d 条安全格式候选；尚未实测连通性", len(nodes)), ""); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) ListNodes() ([]domain.Node, error) {
	rows, err := s.db.Query(`SELECT id,hostname,ip,country,country_name,ping,speed,sessions,source,fetched_at FROM nodes ORDER BY country,hostname LIMIT 2000`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Node{}
	for rows.Next() {
		var n domain.Node
		var fetched string
		if err = rows.Scan(&n.ID, &n.Hostname, &n.IP, &n.Country, &n.CountryName, &n.AdvertisedPing, &n.AdvertisedSpeed, &n.Sessions, &n.Source, &fetched); err != nil {
			return nil, err
		}
		n.Type = "unknown"
		n.Status = "untested"
		n.FetchedAt, _ = time.Parse(time.RFC3339Nano, fetched)
		out = append(out, n)
	}
	return out, rows.Err()
}
