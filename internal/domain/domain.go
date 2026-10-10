package domain

import (
	"errors"
	"regexp"
	"strings"
	"time"
)

const Version = "0.1.0-dev.9"

var ErrNotFound = errors.New("not found")
var ErrConflict = errors.New("conflict")
var countryPattern = regexp.MustCompile(`^[A-Z]{2}$`)

type Exit struct {
	UpstreamKind string    `json:"upstream_kind,omitempty"`
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Country      string    `json:"country"`
	Policy       string    `json:"policy"`
	Port         int       `json:"port"`
	Listen       string    `json:"listen"`
	Username     string    `json:"username"`
	Status       string    `json:"status"`
	Reason       string    `json:"reason"`
	CreatedAt    time.Time `json:"created_at"`
}
type ExitInput struct {
	Name    string `json:"name"`
	Country string `json:"country"`
	Policy  string `json:"policy"`
	Port    int    `json:"port"`
}

func (e *ExitInput) Validate() error {
	e.Name = strings.TrimSpace(e.Name)
	e.Country = strings.ToUpper(strings.TrimSpace(e.Country))
	if len([]rune(e.Name)) < 1 || len([]rune(e.Name)) > 48 {
		return errors.New("出口名称需为 1–48 个字符")
	}
	for _, c := range e.Name {
		if c < 32 || c == 127 {
			return errors.New("名称不能含控制字符")
		}
	}
	if !countryPattern.MatchString(e.Country) {
		return errors.New("国家必须是两位字母代码")
	}
	if e.Policy == "" {
		e.Policy = "any"
	}
	if e.Policy != "any" && e.Policy != "residential_preferred" && e.Policy != "residential_strict" {
		return errors.New("未知节点类型策略")
	}
	if e.Port != 0 && (e.Port < 1024 || e.Port > 65535) {
		return errors.New("端口需在 1024–65535 之间，或留空自动分配")
	}
	return nil
}

type Event struct {
	ID        int64     `json:"id"`
	Level     string    `json:"level"`
	Kind      string    `json:"kind"`
	Message   string    `json:"message"`
	ExitID    string    `json:"exit_id,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}
type Node struct {
	ID              string    `json:"id"`
	Hostname        string    `json:"hostname"`
	IP              string    `json:"ip"`
	Country         string    `json:"country"`
	CountryName     string    `json:"country_name"`
	AdvertisedPing  int       `json:"advertised_ping"`
	AdvertisedSpeed int64     `json:"advertised_speed"`
	Sessions        int       `json:"sessions"`
	Type            string    `json:"type"`
	Status          string    `json:"status"`
	Source          string    `json:"source"`
	FetchedAt       time.Time `json:"fetched_at"`
	Config          string    `json:"-"`
}
type Session struct {
	Username, CSRF string
	ExpiresAt      time.Time
}
