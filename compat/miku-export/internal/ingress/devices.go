package ingress

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"
)

const MaxDevices = 32

var DeviceIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{22}$`)
var ErrDeviceUnavailable = errors.New("设备不存在或已撤销")
var ErrDeviceCapacity = errors.New("每个入口最多32份设备记录；请先移除已撤销记录")

// Device credentials live only inside the authenticated managed-ingress vault.
// HTTP metadata must use View, never serialize Device itself.
type Device struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	CreatedAt time.Time  `json:"created_at"`
	RevokedAt *time.Time `json:"revoked_at,omitempty"`
	UUID      string     `json:"uuid"`
	Password  string     `json:"password"`
}

type DeviceView struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	CreatedAt time.Time  `json:"created_at"`
	RevokedAt *time.Time `json:"revoked_at,omitempty"`
	Status    string     `json:"status"`
}

func DeviceName(value string) (string, error) {
	name := strings.TrimSpace(value)
	if len([]rune(name)) < 1 || len([]rune(name)) > 48 {
		return "", errors.New("设备名称需为1–48个字符")
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return "", errors.New("设备名称不能含控制字符")
		}
	}
	return name, nil
}

func NewDevice(value string) (Device, error) {
	name, err := DeviceName(value)
	if err != nil {
		return Device{}, err
	}
	id, password := make([]byte, 16), make([]byte, 24)
	var uuid [16]byte
	for _, raw := range [][]byte{id, password, uuid[:]} {
		if _, err = rand.Read(raw); err != nil {
			return Device{}, err
		}
	}
	uuid[6], uuid[8] = uuid[6]&15|64, uuid[8]&63|128
	return Device{
		ID: base64.RawURLEncoding.EncodeToString(id), Name: name, CreatedAt: time.Now().UTC(),
		UUID:     fmt.Sprintf("%x-%x-%x-%x-%x", uuid[:4], uuid[4:6], uuid[6:8], uuid[8:10], uuid[10:]),
		Password: base64.RawURLEncoding.EncodeToString(password),
	}, nil
}

func (d Device) View() DeviceView {
	status := "active"
	if d.RevokedAt != nil {
		status = "revoked"
	}
	return DeviceView{d.ID, d.Name, d.CreatedAt, d.RevokedAt, status}
}

func (m ManagedMaterial) ValidateDevices() error {
	if len(m.Devices) > MaxDevices {
		return ErrDeviceCapacity
	}
	ids, uuids, passwords := map[string]bool{}, map[string]bool{m.Identity.UUID: true}, map[string]bool{m.Identity.Password: true}
	for _, d := range m.Devices {
		name, err := DeviceName(d.Name)
		identity := ManagedIdentity{UUID: d.UUID, Password: d.Password}
		raw, decodeErr := base64.RawURLEncoding.DecodeString(d.Password)
		if err != nil || name != d.Name || !DeviceIDPattern.MatchString(d.ID) || d.CreatedAt.IsZero() ||
			(d.RevokedAt != nil && d.RevokedAt.Before(d.CreatedAt)) || identity.Validate(false) != nil ||
			decodeErr != nil || len(raw) != 24 || base64.RawURLEncoding.EncodeToString(raw) != d.Password ||
			ids[d.ID] || uuids[d.UUID] || passwords[d.Password] {
			return ErrConfig
		}
		ids[d.ID], uuids[d.UUID], passwords[d.Password] = true, true, true
	}
	return nil
}

func (m ManagedMaterial) DeviceViews() []DeviceView {
	views := []DeviceView{}
	for _, d := range m.Devices {
		views = append(views, d.View())
	}
	return views
}

func (m ManagedMaterial) ActiveClientCount() int {
	n := 0
	if !m.LegacyDisabled {
		n++
	}
	for _, d := range m.Devices {
		if d.RevokedAt == nil {
			n++
		}
	}
	return n
}

// Empty ID explicitly selects the legacy credential, never a fallback for a
// missing/revoked device. REALITY transport keys remain owned by the entrance.
func (m ManagedMaterial) ClientBundleForDevice(id string) (ClientBundle, error) {
	if id == "" {
		return m.ClientBundle()
	}
	if m.ValidateDevices() != nil {
		return ClientBundle{}, ErrConfig
	}
	for _, d := range m.Devices {
		if d.ID != id {
			continue
		}
		if d.RevokedAt != nil {
			return ClientBundle{}, ErrDeviceUnavailable
		}
		plan, err := ManagedPlanFor(m.Request)
		if err != nil {
			return ClientBundle{}, err
		}
		identity := m.Identity
		identity.UUID, identity.Password = d.UUID, d.Password
		raw, uri, err := renderClient(plan, identity)
		if err != nil {
			return ClientBundle{}, err
		}
		link, err := url.Parse(uri)
		if err != nil {
			return ClientBundle{}, ErrConfig
		}
		link.Fragment = "Miku Gate " + d.Name + " · " + m.Request.Core
		return ClientBundle{ClientJSON: raw, URI: link.String()}, nil
	}
	return ClientBundle{}, ErrDeviceUnavailable
}

func (m ManagedMaterial) renderDevices(config map[string]any) {
	in := object(array(config["inbounds"])[0])
	users := []any{}
	add := func(uuid, password, label string) {
		user := map[string]any{}
		if m.Request.Core == "xray" {
			user["email"] = label + "@miku.invalid"
			if m.Request.Template == "trojan-tls" {
				user["password"] = password
			} else {
				user["id"], user["flow"] = uuid, "xtls-rprx-vision"
			}
		} else {
			user["name"] = label
			if m.Request.Template == "trojan-tls" {
				user["password"] = password
			} else {
				user["uuid"], user["flow"] = uuid, "xtls-rprx-vision"
			}
		}
		users = append(users, user)
	}
	if !m.LegacyDisabled {
		add(m.Identity.UUID, m.Identity.Password, "legacy")
	}
	for _, d := range m.Devices {
		if d.RevokedAt == nil {
			add(d.UUID, d.Password, "device-"+d.ID)
		}
	}
	if m.Request.Core == "xray" {
		object(in["settings"])["clients"] = users
	} else {
		in["users"] = users
	}
}
