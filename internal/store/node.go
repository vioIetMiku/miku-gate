package store

import (
	"database/sql"
	"errors"
	"mikugate/internal/domain"
	"time"
)

// NodeForRuntime is internal-only. Config is excluded from domain.Node JSON and
// limited even if a damaged database contains an unexpectedly large value.
func (s *Store) NodeForRuntime(id string) (domain.Node, error) {
	var node domain.Node
	var fetched string
	err := s.db.QueryRow(`SELECT id,hostname,ip,country,country_name,ping,speed,sessions,source,fetched_at,substr(config,1,131073) FROM nodes WHERE id=?`, id).Scan(&node.ID, &node.Hostname, &node.IP, &node.Country, &node.CountryName, &node.AdvertisedPing, &node.AdvertisedSpeed, &node.Sessions, &node.Source, &fetched, &node.Config)
	if errors.Is(err, sql.ErrNoRows) {
		return node, domain.ErrNotFound
	}
	if err != nil {
		return node, err
	}
	node.FetchedAt, _ = time.Parse(time.RFC3339Nano, fetched)
	node.Type = "unknown"
	node.Status = "untested"
	return node, nil
}
