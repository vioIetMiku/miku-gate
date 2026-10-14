package store

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"mikugate/internal/domain"
	"mikugate/internal/secretstore"
	"mikugate/internal/security"
)

const subscriptionPrefix = "subscription-v1:"
const maxSubscriptions = 64

type privateSubscription struct {
	Subscription domain.Subscription `json:"subscription"`
	TokenHash    string              `json:"token_hash"`
}

func (s *Store) subscriptionLabel(id string) (string, error) {
	instance, err := s.InstanceID()
	return "miku-subscription-v1:" + instance + ":" + id, err
}

func (s *Store) sealSubscription(record privateSubscription) (string, error) {
	if record.Subscription.Validate() != nil {
		return "", domain.ErrSubscriptionUnavailable
	}
	if raw, err := hex.DecodeString(record.TokenHash); err != nil || len(raw) != 32 || hex.EncodeToString(raw) != record.TokenHash {
		return "", domain.ErrSubscriptionUnavailable
	}
	vault, err := s.secretVault()
	if err != nil {
		return "", err
	}
	label, err := s.subscriptionLabel(record.Subscription.ID)
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(record)
	if err != nil {
		return "", err
	}
	defer clear(raw)
	sealed, err := vault.Seal(label, raw)
	return base64.StdEncoding.EncodeToString(sealed), err
}

func (s *Store) openSubscription(id, encoded string) (privateSubscription, error) {
	var record privateSubscription
	if !domain.SubscriptionIDPattern.MatchString(id) || len(encoded) > 16<<10 {
		return record, secretstore.ErrCiphertext
	}
	sealed, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return record, secretstore.ErrCiphertext
	}
	vault, err := s.secretVault()
	if err != nil {
		return record, err
	}
	label, err := s.subscriptionLabel(id)
	if err != nil {
		return record, err
	}
	plain, err := vault.Open(label, sealed)
	if err != nil {
		return record, err
	}
	defer clear(plain)
	if json.Unmarshal(plain, &record) != nil || record.Subscription.ID != id || record.Subscription.Validate() != nil {
		return privateSubscription{}, secretstore.ErrCiphertext
	}
	hash, err := hex.DecodeString(record.TokenHash)
	if err != nil || len(hash) != 32 || hex.EncodeToString(hash) != record.TokenHash {
		return privateSubscription{}, secretstore.ErrCiphertext
	}
	return record, nil
}

func (s *Store) SubscriptionIDs() ([]string, error) {
	rows, err := s.db.Query("SELECT key FROM metadata WHERE key LIKE 'subscription-v1:%' ORDER BY key LIMIT 65")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var key string
		if err = rows.Scan(&key); err != nil {
			return nil, err
		}
		id := strings.TrimPrefix(key, subscriptionPrefix)
		if !domain.SubscriptionIDPattern.MatchString(id) {
			return nil, secretstore.ErrCiphertext
		}
		ids = append(ids, id)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if len(ids) > maxSubscriptions {
		return nil, domain.ErrSubscriptionCapacity
	}
	return ids, nil
}

func (s *Store) privateSubscription(id string) (privateSubscription, error) {
	if !domain.SubscriptionIDPattern.MatchString(id) {
		return privateSubscription{}, domain.ErrNotFound
	}
	var encoded string
	err := s.db.QueryRow("SELECT value FROM metadata WHERE key=?", subscriptionPrefix+id).Scan(&encoded)
	if errors.Is(err, sql.ErrNoRows) {
		return privateSubscription{}, domain.ErrNotFound
	}
	if err != nil {
		return privateSubscription{}, err
	}
	return s.openSubscription(id, encoded)
}

func (s *Store) Subscription(id string) (domain.Subscription, error) {
	record, err := s.privateSubscription(id)
	return record.Subscription, err
}

func (s *Store) ListSubscriptions() ([]domain.SubscriptionView, error) {
	ids, err := s.SubscriptionIDs()
	if err != nil {
		return nil, err
	}
	views := []domain.SubscriptionView{}
	for _, id := range ids {
		record, err := s.privateSubscription(id)
		if err != nil {
			return nil, err
		}
		views = append(views, record.Subscription.View(time.Now()))
	}
	sort.Slice(views, func(i, j int) bool { return views[i].CreatedAt.After(views[j].CreatedAt) })
	return views, nil
}

func (s *Store) CreateSubscription(ctx context.Context, input domain.SubscriptionInput) (domain.Subscription, string, error) {
	if err := input.Validate(); err != nil {
		return domain.Subscription{}, "", err
	}
	if err := ctx.Err(); err != nil {
		return domain.Subscription{}, "", err
	}
	// Existing authenticated material must be readable, not merely an exit row.
	revisions := map[string]string{}
	for _, id := range input.ExitIDs {
		material, _, ok, revision, err := s.ManagedSnapshot(id)
		if err != nil || !ok {
			return domain.Subscription{}, "", domain.ErrSubscriptionUnavailable
		}
		if _, err = material.ClientBundleForDevice(input.DeviceIDs[id]); err != nil {
			return domain.Subscription{}, "", domain.ErrSubscriptionUnavailable
		}
		revisions[id] = revision
	}
	id, err := security.Token(16)
	if err != nil {
		return domain.Subscription{}, "", err
	}
	token, err := security.Token(32)
	if err != nil {
		return domain.Subscription{}, "", err
	}
	now := time.Now().UTC()
	item := domain.Subscription{ID: id, Name: input.Name, ExitIDs: input.ExitIDs, CreatedAt: now, ExpiresAt: now.Add(time.Duration(input.TTLHours) * time.Hour), DeviceIDs: input.DeviceIDs}
	sealed, err := s.sealSubscription(privateSubscription{item, security.Digest(token)})
	if err != nil {
		return domain.Subscription{}, "", err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Subscription{}, "", err
	}
	defer tx.Rollback()
	var count int
	if err = tx.QueryRow("SELECT count(*) FROM metadata WHERE key LIKE 'subscription-v1:%'").Scan(&count); err != nil {
		return domain.Subscription{}, "", err
	}
	if count >= maxSubscriptions {
		return domain.Subscription{}, "", domain.ErrSubscriptionCapacity
	}
	for _, exitID := range input.ExitIDs {
		var current []byte
		if err = tx.QueryRow("SELECT sealed FROM managed_ingress WHERE exit_id=?", exitID).Scan(&current); err != nil || sealedRevision(current) != revisions[exitID] {
			return domain.Subscription{}, "", domain.ErrSubscriptionUnavailable
		}
	}
	if _, err = tx.Exec("INSERT INTO metadata(key,value) VALUES(?,?)", subscriptionPrefix+id, sealed); err != nil {
		return domain.Subscription{}, "", err
	}
	if err = event(tx, "info", "subscription.created", "已创建限时、限定出口的订阅访问权限；令牌不写审计", ""); err != nil {
		return domain.Subscription{}, "", err
	}
	if err = tx.Commit(); err != nil {
		return domain.Subscription{}, "", err
	}
	return item, token, nil
}

func (s *Store) SubscriptionForToken(token string) (domain.Subscription, error) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) != 32 || base64.RawURLEncoding.EncodeToString(raw) != token {
		return domain.Subscription{}, domain.ErrSubscriptionUnavailable
	}
	ids, err := s.SubscriptionIDs()
	if err != nil {
		return domain.Subscription{}, domain.ErrSubscriptionUnavailable
	}
	hash := security.Digest(token)
	for _, id := range ids {
		record, err := s.privateSubscription(id)
		if err != nil {
			return domain.Subscription{}, domain.ErrSubscriptionUnavailable
		}
		if subtle.ConstantTimeCompare([]byte(hash), []byte(record.TokenHash)) == 1 {
			if record.Subscription.Status(time.Now()) != "active" {
				return domain.Subscription{}, domain.ErrSubscriptionUnavailable
			}
			return record.Subscription, nil
		}
	}
	return domain.Subscription{}, domain.ErrSubscriptionUnavailable
}

func (s *Store) RevokeSubscription(ctx context.Context, id string) (domain.Subscription, error) {
	record, err := s.privateSubscription(id)
	if err != nil {
		return domain.Subscription{}, err
	}
	if record.Subscription.RevokedAt != nil {
		return record.Subscription, nil
	}
	now := time.Now().UTC()
	record.Subscription.RevokedAt = &now
	sealed, err := s.sealSubscription(record)
	if err != nil {
		return domain.Subscription{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Subscription{}, err
	}
	defer tx.Rollback()
	result, err := tx.Exec("UPDATE metadata SET value=? WHERE key=?", sealed, subscriptionPrefix+id)
	if err != nil {
		return domain.Subscription{}, err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return domain.Subscription{}, domain.ErrNotFound
	}
	if err = event(tx, "info", "subscription.revoked", "订阅访问权限已撤销；已经导入的入口凭据不因此改变", ""); err != nil {
		return domain.Subscription{}, err
	}
	if err = tx.Commit(); err != nil {
		return domain.Subscription{}, err
	}
	return record.Subscription, nil
}

func (s *Store) DeleteSubscription(ctx context.Context, id string) error {
	record, err := s.privateSubscription(id)
	if err != nil {
		return err
	}
	if record.Subscription.Status(time.Now()) == "active" {
		return domain.ErrConflict
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.Exec("DELETE FROM metadata WHERE key=?", subscriptionPrefix+id)
	if err != nil {
		return err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return domain.ErrNotFound
	}
	if err = event(tx, "info", "subscription.deleted", "已移除失效订阅记录，旧令牌继续不可用", ""); err != nil {
		return err
	}
	return tx.Commit()
}
