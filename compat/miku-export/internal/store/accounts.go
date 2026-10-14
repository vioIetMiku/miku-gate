package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"regexp"

	"mikugate/internal/domain"
	"mikugate/internal/security"
)

type Account struct {
	Username string   `json:"username"`
	Role     string   `json:"role"`
	ExitIDs  []string `json:"exit_ids"`
}

func (s *Store) Accounts() ([]Account, error) {
	rows, err := s.db.Query("SELECT a.username,COALESCE(p.role,'viewer'),COALESCE(p.scope,'[]') FROM accounts a LEFT JOIN account_policies p ON p.username=a.username ORDER BY a.username LIMIT 32")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Account{}
	for rows.Next() {
		var a Account
		var scope string
		if err = rows.Scan(&a.Username, &a.Role, &scope); err != nil {
			return nil, err
		}
		if json.Unmarshal([]byte(scope), &a.ExitIDs) != nil {
			return nil, errors.New("账号范围无效")
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
func (s *Store) validateAccount(a Account) error {
	if !regexp.MustCompile(`^[A-Za-z0-9_-]{1,40}$`).MatchString(a.Username) || (a.Role != "admin" && a.Role != "operator" && a.Role != "viewer") || len(a.ExitIDs) > 100 {
		return errors.New("账号/角色/出口范围无效")
	}
	if a.Role != "admin" && len(a.ExitIDs) == 0 {
		return errors.New("普通账号需限定至少1个出口")
	}
	seen := map[string]bool{}
	for _, id := range a.ExitIDs {
		if seen[id] {
			return errors.New("出口重复")
		}
		seen[id] = true
		if _, _, err := s.ExitForRuntime(id); err != nil {
			return err
		}
	}
	return nil
}
func (s *Store) CreateAccount(a Account) (string, error) {
	if err := s.validateAccount(a); err != nil {
		return "", err
	}
	password, err := security.Token(18)
	if err != nil {
		return "", err
	}
	hash, err := security.HashPassword(password)
	if err != nil {
		return "", err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var count int
	if err = tx.QueryRow("SELECT count(*) FROM accounts").Scan(&count); err != nil {
		return "", err
	}
	if count >= 32 {
		return "", errors.New("最多32个管理账号")
	}
	if _, err = tx.Exec("INSERT INTO accounts(username,password_hash) VALUES(?,?)", a.Username, hash); err != nil {
		return "", domain.ErrConflict
	}
	if a.ExitIDs == nil {
		a.ExitIDs = []string{}
	}
	scope, _ := json.Marshal(a.ExitIDs)
	if _, err = tx.Exec("INSERT INTO account_policies(username,role,scope) VALUES(?,?,?)", a.Username, a.Role, string(scope)); err != nil {
		return "", err
	}
	if err = event(tx, "info", "account.created", "已创建独立管理账号及明确角色/出口范围", ""); err != nil {
		return "", err
	}
	return password, tx.Commit()
}
func (s *Store) ChangeAccount(a Account, actor string, remove bool) error {
	if a.Username == actor {
		return errors.New("当前账号不能修改自己的权限或删除自己")
	}
	if !remove {
		if err := s.validateAccount(a); err != nil {
			return err
		}
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var role string
	if err = tx.QueryRow("SELECT role FROM account_policies WHERE username=?", a.Username).Scan(&role); errors.Is(err, sql.ErrNoRows) {
		return domain.ErrNotFound
	} else if err != nil {
		return err
	}
	if role == "admin" && (remove || a.Role != "admin") {
		var n int
		if err = tx.QueryRow("SELECT count(*) FROM account_policies WHERE role='admin'").Scan(&n); err != nil {
			return err
		}
		if n <= 1 {
			return errors.New("必须保留至少一个管理员")
		}
	}
	if _, err = tx.Exec("DELETE FROM sessions WHERE username=?", a.Username); err != nil {
		return err
	}
	if remove {
		_, err = tx.Exec("DELETE FROM accounts WHERE username=?", a.Username)
	} else {
		if a.ExitIDs == nil {
			a.ExitIDs = []string{}
		}
		scope, _ := json.Marshal(a.ExitIDs)
		_, err = tx.Exec("UPDATE account_policies SET role=?,scope=? WHERE username=?", a.Role, string(scope), a.Username)
	}
	if err != nil {
		return err
	}
	if err = event(tx, "info", "account.permissions_changed", "账号权限已调整或撤销，旧会话已退出", ""); err != nil {
		return err
	}
	return tx.Commit()
}
