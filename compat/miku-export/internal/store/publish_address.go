package store

import (
	"encoding/json"

	"mikugate/internal/certassets"
	"mikugate/internal/domain"
	"mikugate/internal/ingress"
)

// Called under the ordinary account's exclusive data lease after a root
// domain transaction stops the service. Client IDs/passwords stay unchanged.
func (s *Store) PublishEntranceAddress(address string, asset *certassets.Asset) (int, error) {
	ids, err := s.EntranceIDs()
	if err != nil {
		return 0, err
	}
	count := 0
	for _, id := range ids {
		view, material, err := s.Entrance(id)
		if err != nil {
			return count, err
		}
		material.Request.Address = address
		if asset != nil && material.Request.Template != "vless-reality-vision" && material.External == nil {
			material.Request.ServerName = asset.Summary().Hostname
			material.CertificatePEM, material.PrivateKeyPEM = asset.PEM()
		}
		if err = s.UpdateEntrance(id, view.Name, view.Revision, material); err != nil {
			return count, err
		}
		count++
	}
	rows, err := s.db.Query("SELECT exit_id FROM managed_ingress")
	if err != nil {
		return count, err
	}
	oldIDs := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return count, err
		}
		oldIDs = append(oldIDs, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return count, err
	}
	for _, id := range oldIDs {
		material, _, ok, revision, err := s.ManagedSnapshot(id)
		if err != nil || !ok {
			return count, err
		}
		material.Request.Address = address
		if asset != nil && material.Request.Template != "vless-reality-vision" {
			material.Request.ServerName = asset.Summary().Hostname
			material.CertificatePEM, material.PrivateKeyPEM = asset.PEM()
		}
		if _, err = ingress.ManagedPlanFor(material.Request); err != nil {
			return count, err
		}
		raw, err := json.Marshal(material)
		if err != nil {
			return count, err
		}
		vault, err := s.secretVault()
		if err != nil {
			clear(raw)
			return count, err
		}
		label, err := s.managedLabel(id)
		if err != nil {
			clear(raw)
			return count, err
		}
		sealed, err := vault.Seal(label, raw)
		clear(raw)
		if err != nil {
			return count, err
		}
		tx, err := s.db.Begin()
		if err != nil {
			return count, err
		}
		var current []byte
		if err = tx.QueryRow("SELECT sealed FROM managed_ingress WHERE exit_id=?", id).Scan(&current); err != nil || sealedRevision(current) != revision {
			tx.Rollback()
			if err != nil {
				return count, err
			}
			return count, domain.ErrConflict
		}
		_, err = tx.Exec("UPDATE managed_ingress SET sealed=? WHERE exit_id=?", sealed, id)
		if err != nil {
			tx.Rollback()
			return count, err
		}
		if err = tx.Commit(); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}
