package store

import "mikugate/internal/ingress"

type ClientEntry struct {
	ExitID          string                 `json:"exit_id"`
	Name            string                 `json:"name"`
	Request         ingress.ManagedRequest `json:"request"`
	UpdatedAt       string                 `json:"updated_at"`
	ClientAvailable bool                   `json:"client_available"`
	Revision        string                 `json:"revision"`
	LegacyEnabled   bool                   `json:"legacy_enabled"`
	Devices         []ingress.DeviceView   `json:"devices"`
}

func (s *Store) ClientEntries() ([]ClientEntry, error) {
	rows, err := s.db.Query("SELECT e.id,e.name FROM exits e JOIN managed_ingress m ON m.exit_id=e.id ORDER BY e.created_at LIMIT 101")
	if err != nil {
		return nil, err
	}
	entries := []ClientEntry{}
	for rows.Next() {
		var item ClientEntry
		if err = rows.Scan(&item.ExitID, &item.Name); err != nil {
			rows.Close()
			return nil, err
		}
		entries = append(entries, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for n := range entries {
		material, updated, ok, revision, err := s.ManagedSnapshot(entries[n].ExitID)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		entries[n].Request = material.Request
		entries[n].UpdatedAt = updated
		entries[n].Revision = revision
		entries[n].LegacyEnabled = !material.LegacyDisabled
		entries[n].Devices = material.DeviceViews()
		plan, err := ingress.ManagedPlanFor(material.Request)
		entries[n].ClientAvailable = err == nil && material.Identity.Validate(plan.NeedsRealityTarget) == nil && !material.LegacyDisabled
	}
	return entries, nil
}
