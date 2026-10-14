package domain

import "time"

type NodePreference struct {
	NodeID    string    `json:"node_id"`
	Favorite  bool      `json:"favorite"`
	Blocked   bool      `json:"blocked"`
	Tags      []string  `json:"tags"`
	Note      string    `json:"note"`
	UpdatedAt time.Time `json:"updated_at"`
}

type HistorySample struct {
	ID          int64     `json:"id"`
	ExitID      string    `json:"exit_id"`
	Phase       string    `json:"phase"`
	Healthy     bool      `json:"healthy"`
	ExitIP      string    `json:"exit_ip,omitempty"`
	CheckMS     int64     `json:"check_ms"`
	Connections int64     `json:"connections"`
	Upload      int64     `json:"upload_bytes"`
	Download    int64     `json:"download_bytes"`
	RunSince    string    `json:"run_since,omitempty"`
	RecordedAt  time.Time `json:"recorded_at"`
}

type AgentToken struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Role      string    `json:"role"`
	ExitIDs   []string  `json:"exit_ids"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

type FleetHost struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Origin       string    `json:"origin"`
	AllowPrivate bool      `json:"allow_private"`
	CA           string    `json:"ca_pem,omitempty"`
	Token        string    `json:"-"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type StandbyPolicy struct {
	Enabled     bool   `json:"enabled"`
	NodeID      string `json:"node_id"`
	AllowLegacy bool   `json:"allow_legacy"`
	Confirm     bool   `json:"confirm"`
}
