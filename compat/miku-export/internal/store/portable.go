package store

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/crypto/argon2"
)

const portableMagic = "MIKUBK01"
const portableLimit = 64 << 20

func portableCipher(pass string, salt []byte) (cipher.AEAD, error) {
	if len(pass) < 12 || len(pass) > 256 {
		return nil, errors.New("备份口令需12–256字节")
	}
	key := argon2.IDKey([]byte(pass), salt, 2, 64*1024, 2, 32)
	defer clear(key)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
func boundedRead(path string, limit int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > limit {
		return nil, errors.New("备份文件类型或大小无效")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	now, err := f.Stat()
	if err != nil || !os.SameFile(info, now) {
		return nil, errors.New("备份文件身份变化")
	}
	raw, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil || int64(len(raw)) > limit {
		return nil, errors.New("备份超过大小限制")
	}
	return raw, nil
}

// ExportPortable creates a consistent SQLite snapshot including its paired key,
// then encrypts the entire ZIP. Plaintext never leaves the private local directory.
func (s *Store) ExportPortable(ctx context.Context, pass string) ([]byte, error) {
	if len(pass) < 12 || len(pass) > 256 {
		return nil, errors.New("备份口令需12–256字节")
	}
	if _, err := s.secretVault(); err != nil {
		return nil, err
	}
	directory, err := os.MkdirTemp(filepath.Dir(s.path), ".portable-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(directory)
	snapshot := filepath.Join(directory, "miku-gate.db")
	if err = s.SnapshotDatabase(ctx, snapshot); err != nil {
		return nil, err
	}
	if err = os.Chmod(snapshot, 0600); err != nil {
		return nil, err
	}
	db, err := boundedRead(snapshot, portableLimit)
	if err != nil {
		return nil, err
	}
	defer clear(db)
	key, err := boundedRead(s.path+".key", 32)
	if err != nil || len(key) != 32 {
		return nil, errors.New("配套数据库密钥无效")
	}
	defer clear(key)
	instance, err := s.InstanceID()
	if err != nil {
		return nil, err
	}
	manifest, _ := json.Marshal(map[string]any{"format": 1, "instance": instance, "created_at": time.Now().UTC(), "scope": "database-and-paired-key", "services_included": false})
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	for _, item := range []struct {
		name string
		data []byte
	}{{"manifest.json", manifest}, {"miku-gate.db", db}, {"miku-gate.db.key", key}} {
		header := &zip.FileHeader{Name: item.name, Method: zip.Deflate}
		header.SetMode(0600)
		writer, err := archive.CreateHeader(header)
		if err != nil {
			return nil, err
		}
		if _, err = writer.Write(item.data); err != nil {
			return nil, err
		}
	}
	if err = archive.Close(); err != nil {
		return nil, err
	}
	plain := buffer.Bytes()
	defer clear(plain)
	if len(plain) > portableLimit+8192 {
		return nil, errors.New("备份压缩结果超预算")
	}
	salt := make([]byte, 16)
	if _, err = rand.Read(salt); err != nil {
		return nil, err
	}
	aead, err := portableCipher(pass, salt)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return nil, err
	}
	header := append([]byte(portableMagic), salt...)
	header = append(header, nonce...)
	return aead.Seal(header, nonce, plain, header), nil
}

// RestorePortable only creates a new private directory. No service is started,
// no existing DB is overwritten, and host-local authorizations are withdrawn.
func RestorePortable(ctx context.Context, raw []byte, pass, destination string) (MaintenanceInspection, error) {
	var report MaintenanceInspection
	if len(raw) < 52 || len(raw) > portableLimit+16384 || string(raw[:8]) != portableMagic {
		return report, errors.New("备份格式无效")
	}
	aead, err := portableCipher(pass, raw[8:24])
	if err != nil {
		return report, err
	}
	header := raw[:36]
	plain, err := aead.Open(nil, raw[24:36], raw[36:], header)
	if err != nil {
		return report, errors.New("备份口令错误或文件损坏")
	}
	defer clear(plain)
	archive, err := zip.NewReader(bytes.NewReader(plain), int64(len(plain)))
	if err != nil || len(archive.File) != 3 {
		return report, errors.New("备份归档无效")
	}
	files := map[string][]byte{}
	defer func() {
		for _, data := range files {
			clear(data)
		}
	}()
	for _, item := range archive.File {
		limit := int64(4096)
		switch item.Name {
		case "miku-gate.db":
			limit = portableLimit
		case "miku-gate.db.key":
			limit = 32
		case "manifest.json":
		default:
			return report, errors.New("备份包含未授权文件")
		}
		if item.UncompressedSize64 > uint64(limit) || files[item.Name] != nil {
			return report, errors.New("备份大小或重复成员无效")
		}
		reader, err := item.Open()
		if err != nil {
			return report, err
		}
		data, readErr := io.ReadAll(io.LimitReader(reader, limit+1))
		closeErr := reader.Close()
		if readErr != nil || closeErr != nil || int64(len(data)) > limit {
			return report, errors.New("备份成员读取失败")
		}
		files[item.Name] = data
	}
	var manifest struct {
		Format   int    `json:"format"`
		Instance string `json:"instance"`
	}
	if json.Unmarshal(files["manifest.json"], &manifest) != nil || manifest.Format != 1 || len(files["miku-gate.db.key"]) != 32 {
		return report, errors.New("备份清单或密钥无效")
	}
	if ctx.Err() != nil {
		return report, ctx.Err()
	}
	absolute, err := filepath.Abs(destination)
	if err != nil {
		return report, err
	}
	if err = os.Mkdir(absolute, 0700); err != nil {
		return report, errors.New("目标必须是尚不存在的新目录，且父目录已存在")
	}
	success := false
	defer func() {
		if !success {
			os.RemoveAll(absolute)
		}
	}()
	for _, name := range []string{"miku-gate.db", "miku-gate.db.key"} {
		file, err := os.OpenFile(filepath.Join(absolute, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return report, err
		}
		_, err = file.Write(files[name])
		syncErr := file.Sync()
		closeErr := file.Close()
		if err != nil || syncErr != nil || closeErr != nil {
			return report, errors.New("恢复写入失败")
		}
	}
	report, err = InspectMaintenance(filepath.Join(absolute, "miku-gate.db"))
	if err != nil || report.Instance != manifest.Instance {
		return report, errors.New("恢复后数据库/密钥/身份检查失败")
	}
	restored, err := Open(filepath.Join(absolute, "miku-gate.db"))
	if err != nil {
		return report, err
	}
	defer restored.Close()
	tx, err := restored.db.BeginTx(ctx, nil)
	if err != nil {
		return report, err
	}
	defer tx.Rollback()
	for _, query := range []string{"DELETE FROM sessions", "DELETE FROM agent_tokens", "DELETE FROM metadata WHERE key LIKE 'standby-v1:%' OR key LIKE 'recovery-v1:%' OR key LIKE 'task-v1:%' OR key LIKE 'panel-backup-v1:%' OR key='panel-pending-v1'", "UPDATE exits SET status='stopped',reason='已从可迁移备份恢复，自动恢复和Agent令牌已取消，请确认新主机环境后手动启动'"} {
		if _, err = tx.ExecContext(ctx, query); err != nil {
			return report, err
		}
	}
	if err = event(tx, "info", "backup.portable_restored", "已恢复数据库及配套密钥；入口/设备身份保留，服务未启动，主机授权已撤销", ""); err != nil {
		return report, err
	}
	if err = tx.Commit(); err != nil {
		return report, err
	}
	success = true
	return report, nil
}
