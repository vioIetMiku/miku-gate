package domain

import (
	"errors"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
)

var ErrSubscriptionUnavailable = errors.New("subscription unavailable")
var ErrSubscriptionCapacity = errors.New("subscription capacity")
var SubscriptionIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{22}$`)
var subscriptionExitPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{16}$`)

type SubscriptionInput struct {
	Name      string            `json:"name"`
	ExitIDs   []string          `json:"exit_ids"`
	TTLHours  int               `json:"ttl_hours"`
	DeviceIDs map[string]string `json:"device_ids,omitempty"`
}

func (i *SubscriptionInput) Validate() error {
	i.Name = strings.TrimSpace(i.Name)
	if len([]rune(i.Name)) < 1 || len([]rune(i.Name)) > 48 {
		return errors.New("订阅名称需为1–48个字符")
	}
	for _, r := range i.Name {
		if unicode.IsControl(r) {
			return errors.New("订阅名称不能含控制字符")
		}
	}
	if i.TTLHours < 1 || i.TTLHours > 720 || len(i.ExitIDs) < 1 || len(i.ExitIDs) > 32 {
		return errors.New("请选择1–32个已保存入口，有效期1–720小时")
	}
	i.ExitIDs = append([]string(nil), i.ExitIDs...)
	sort.Strings(i.ExitIDs)
	for n, id := range i.ExitIDs {
		if !subscriptionExitPattern.MatchString(id) || (n > 0 && id == i.ExitIDs[n-1]) {
			return errors.New("订阅出口身份无效或重复")
		}
	}
	if len(i.DeviceIDs) > len(i.ExitIDs) {
		return errors.New("设备授权须属于所选入口")
	}
	devices := make(map[string]string, len(i.DeviceIDs))
	for id, device := range i.DeviceIDs {
		n := sort.SearchStrings(i.ExitIDs, id)
		if n == len(i.ExitIDs) || i.ExitIDs[n] != id || !SubscriptionIDPattern.MatchString(device) {
			return errors.New("设备授权须属于所选入口且身份有效")
		}
		devices[id] = device
	}
	i.DeviceIDs = devices
	return nil
}

// Subscription is safe administrator metadata. The bearer token and its hash
// are never part of this public view.
type Subscription struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	ExitIDs   []string          `json:"exit_ids"`
	CreatedAt time.Time         `json:"created_at"`
	ExpiresAt time.Time         `json:"expires_at"`
	RevokedAt *time.Time        `json:"revoked_at,omitempty"`
	DeviceIDs map[string]string `json:"device_ids,omitempty"`
}

func (s Subscription) Validate() error {
	if !SubscriptionIDPattern.MatchString(s.ID) || s.CreatedAt.IsZero() || !s.ExpiresAt.After(s.CreatedAt) || s.ExpiresAt.Sub(s.CreatedAt) > 720*time.Hour || (s.RevokedAt != nil && s.RevokedAt.Before(s.CreatedAt)) {
		return ErrSubscriptionUnavailable
	}
	i := SubscriptionInput{Name: s.Name, ExitIDs: s.ExitIDs, TTLHours: 1, DeviceIDs: s.DeviceIDs}
	if i.Validate() != nil || i.Name != s.Name {
		return ErrSubscriptionUnavailable
	}
	return nil
}

func (s Subscription) Status(now time.Time) string {
	if s.RevokedAt != nil {
		return "revoked"
	}
	if now.Before(s.CreatedAt) || !now.Before(s.ExpiresAt) {
		return "expired"
	}
	return "active"
}

type SubscriptionView struct {
	Subscription
	Status string `json:"status"`
}

func (s Subscription) View(now time.Time) SubscriptionView {
	return SubscriptionView{Subscription: s, Status: s.Status(now)}
}
