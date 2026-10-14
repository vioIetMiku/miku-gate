package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"mikugate/internal/domain"
	"mikugate/internal/ingress"
	"mikugate/internal/portalloc"
	"mikugate/internal/secretstore"
	"mikugate/internal/security"
)

type EntranceView struct {
	ID        string                 `json:"id"`
	Name      string                 `json:"name"`
	Source    string                 `json:"source"`
	Request   ingress.ManagedRequest `json:"request"`
	Revision  string                 `json:"revision"`
	UpdatedAt string                 `json:"updated_at"`
	Landings  []domain.Exit          `json:"landings"`
}

func (s *Store) entranceLabel(id string) (string, error) {
	instance, err := s.InstanceID()
	return "miku-entrance-v1:" + instance + ":" + id, err
}

func (s *Store) Entrance(id string) (EntranceView, ingress.ManagedMaterial, error) {
	var view EntranceView
	var material ingress.ManagedMaterial
	var sealed []byte
	var core string
	var port int
	err := s.db.QueryRow("SELECT id,name,source,core,port,sealed,updated_at FROM entrances WHERE id=?", id).Scan(&view.ID, &view.Name, &view.Source, &core, &port, &sealed, &view.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return view, material, domain.ErrNotFound
	}
	if err != nil {
		return view, material, err
	}
	vault, err := s.secretVault()
	if err != nil {
		return view, material, err
	}
	label, err := s.entranceLabel(id)
	if err != nil {
		return view, material, err
	}
	raw, err := vault.Open(label, sealed)
	if err != nil {
		return view, material, err
	}
	defer clear(raw)
	if json.Unmarshal(raw, &material) != nil || material.Request.Core != core || material.Request.Port != port || material.Identity.Validate(material.Request.Template == "vless-reality-vision") != nil {
		return view, material, secretstore.ErrCiphertext
	}
	view.Request, view.Revision, view.Landings = material.Request, sealedRevision(sealed), []domain.Exit{}
	ids, err := s.EntranceExitIDs(id)
	if err != nil {
		return view, material, err
	}
	for _, exitID := range ids {
		exit, _, err := s.ExitForRuntime(exitID)
		if err != nil {
			return view, material, err
		}
		view.Landings = append(view.Landings, exit)
	}
	return view, material, nil
}

func (s *Store) EntranceIDs() ([]string, error) {
	rows, err := s.db.Query("SELECT id FROM entrances ORDER BY updated_at DESC LIMIT 33")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if len(ids) > 32 {
		return nil, domain.ErrConflict
	}
	return ids, rows.Err()
}

func (s *Store) Entrances() ([]EntranceView, error) {
	ids, err := s.EntranceIDs()
	if err != nil {
		return nil, err
	}
	views := []EntranceView{}
	for _, id := range ids {
		view, _, err := s.Entrance(id)
		if err != nil {
			return nil, err
		}
		views = append(views, view)
	}
	return views, nil
}

func (s *Store) FreeEntrancePort() (int, error) {
	for port := 30000; port < 60000; port++ {
		var used int
		if err := s.db.QueryRow("SELECT (SELECT count(*) FROM entrances WHERE port=?)+(SELECT count(*) FROM managed_ingress WHERE port=?)+(SELECT count(*) FROM exits WHERE port=?)", port, port, port).Scan(&used); err != nil {
			return 0, err
		}
		if used == 0 && portalloc.Free(port) {
			return port, nil
		}
	}
	return 0, errors.New("没有可分配的空闲入口端口")
}

func (s *Store) CreateEntrance(name, source string, material ingress.ManagedMaterial) (EntranceView, error) {
	name, err := ingress.DeviceName(name)
	if err != nil {
		return EntranceView{}, err
	}
	if material.External == nil {
		if _, err = material.Render(); err != nil {
			return EntranceView{}, err
		}
	}
	id, err := security.Token(12)
	if err != nil {
		return EntranceView{}, err
	}
	vault, err := s.secretVault()
	if err != nil {
		return EntranceView{}, err
	}
	label, err := s.entranceLabel(id)
	if err != nil {
		return EntranceView{}, err
	}
	raw, err := json.Marshal(material)
	if err != nil {
		return EntranceView{}, err
	}
	defer clear(raw)
	sealed, err := vault.Seal(label, raw)
	if err != nil {
		return EntranceView{}, err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return EntranceView{}, err
	}
	defer tx.Rollback()
	var count, used int
	if err = tx.QueryRow("SELECT count(*) FROM entrances").Scan(&count); err != nil {
		return EntranceView{}, err
	}
	if count >= 32 {
		return EntranceView{}, domain.ErrConflict
	}
	if err = tx.QueryRow("SELECT (SELECT count(*) FROM entrances WHERE port=?)+(SELECT count(*) FROM managed_ingress WHERE port=?)+(SELECT count(*) FROM exits WHERE port=?)", material.Request.Port, material.Request.Port, material.Request.Port).Scan(&used); err != nil {
		return EntranceView{}, err
	}
	if used != 0 || (material.External == nil && !portalloc.Free(material.Request.Port)) {
		return EntranceView{}, errors.New("入口端口已占用，请重新自动分配")
	}
	if _, err = tx.Exec("INSERT INTO entrances VALUES(?,?,?,?,?,?,?)", id, name, source, material.Request.Core, material.Request.Port, sealed, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return EntranceView{}, err
	}
	if err = event(tx, "info", "entrance.created", "已创建入口："+name+"；尚未绑定落地", ""); err != nil {
		return EntranceView{}, err
	}
	if err = tx.Commit(); err != nil {
		return EntranceView{}, err
	}
	view, _, err := s.Entrance(id)
	return view, err
}

func (s *Store) UpdateEntrance(id, name, revision string, material ingress.ManagedMaterial) error {
	name, err := ingress.DeviceName(name)
	if err != nil {
		return err
	}
	old, _, err := s.Entrance(id)
	if err != nil {
		return err
	}
	if old.Revision != revision || (old.Source == "miku" && material.Request.Port != old.Request.Port) {
		return domain.ErrConflict
	}
	if material.External == nil {
		if _, err = material.Render(); err != nil {
			return err
		}
	}
	raw, err := json.Marshal(material)
	if err != nil {
		return err
	}
	defer clear(raw)
	vault, err := s.secretVault()
	if err != nil {
		return err
	}
	label, err := s.entranceLabel(id)
	if err != nil {
		return err
	}
	sealed, err := vault.Seal(label, raw)
	if err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var current []byte
	if err = tx.QueryRow("SELECT sealed FROM entrances WHERE id=?", id).Scan(&current); err != nil {
		return err
	}
	if sealedRevision(current) != revision {
		return domain.ErrConflict
	}
	if material.Request.Port != old.Request.Port {
		var used int
		if err = tx.QueryRow("SELECT (SELECT count(*) FROM entrances WHERE port=? AND id<>?)+(SELECT count(*) FROM managed_ingress WHERE port=?)+(SELECT count(*) FROM exits WHERE port=?)", material.Request.Port, id, material.Request.Port, material.Request.Port).Scan(&used); err != nil {
			return err
		}
		if used != 0 || (material.External == nil && !portalloc.Free(material.Request.Port)) {
			return errors.New("入口端口已占用，请重新自动分配")
		}
	}
	if _, err = tx.Exec("UPDATE entrances SET name=?,source=?,core=?,port=?,sealed=?,updated_at=? WHERE id=?", name, func() string {
		if material.External == nil {
			return "miku"
		}
		return material.External.Kind
	}(), material.Request.Core, material.Request.Port, sealed, time.Now().UTC().Format(time.RFC3339Nano), id); err != nil {
		return err
	}
	if err = event(tx, "info", "entrance.updated", "已保存入口设置，保留各落地客户端身份", ""); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) EntranceExitIDs(id string) ([]string, error) {
	rows, err := s.db.Query("SELECT exit_id FROM entrance_exits WHERE entrance_id=? ORDER BY position,exit_id", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var exitID string
		if err = rows.Scan(&exitID); err != nil {
			return nil, err
		}
		ids = append(ids, exitID)
	}
	return ids, rows.Err()
}

func (s *Store) BindEntranceExit(id, exitID string) error {
	if _, _, err := s.Entrance(id); err != nil {
		return err
	}
	exit, _, err := s.ExitForRuntime(exitID)
	if err != nil {
		return err
	}
	device, err := ingress.NewDevice(exit.Name)
	if err != nil {
		return err
	}
	identity := ingress.ManagedIdentity{UUID: device.UUID, Password: device.Password}
	raw, err := json.Marshal(identity)
	if err != nil {
		return err
	}
	defer clear(raw)
	vault, err := s.secretVault()
	if err != nil {
		return err
	}
	label, err := s.entranceLabel(id)
	if err != nil {
		return err
	}
	sealed, err := vault.Seal(label+":exit:"+exitID, raw)
	if err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var count int
	if err = tx.QueryRow("SELECT count(*) FROM entrance_exits WHERE entrance_id=?", id).Scan(&count); err != nil {
		return err
	}
	if count >= 32 {
		return domain.ErrConflict
	}
	_, err = tx.Exec("INSERT INTO entrance_exits(entrance_id,exit_id,sealed,position) VALUES(?,?,?,?) ON CONFLICT(entrance_id,exit_id) DO NOTHING", id, exitID, sealed, count)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) EntranceRoutes(id string) (EntranceView, ingress.ManagedMaterial, []ingress.LandingRoute, error) {
	view, base, err := s.Entrance(id)
	if err != nil {
		return view, base, nil, err
	}
	routes := []ingress.LandingRoute{}
	vault, err := s.secretVault()
	if err != nil {
		return view, base, nil, err
	}
	label, err := s.entranceLabel(id)
	if err != nil {
		return view, base, nil, err
	}
	for _, exit := range view.Landings {
		var sealed []byte
		if err = s.db.QueryRow("SELECT sealed FROM entrance_exits WHERE entrance_id=? AND exit_id=?", id, exit.ID).Scan(&sealed); err != nil {
			return view, base, nil, err
		}
		raw, err := vault.Open(label+":exit:"+exit.ID, sealed)
		if err != nil {
			return view, base, nil, err
		}
		var identity ingress.ManagedIdentity
		err = json.Unmarshal(raw, &identity)
		clear(raw)
		if err != nil || identity.Validate(false) != nil {
			return view, base, nil, secretstore.ErrCiphertext
		}
		password, err := s.ProxyPassword(exit.ID)
		if err != nil {
			return view, base, nil, err
		}
		routes = append(routes, ingress.LandingRoute{ExitID: exit.ID, Name: view.Name + " · " + exit.Name, Identity: identity, Proxy: ingress.Proxy{Port: exit.Port, Username: exit.Username, Password: password}})
	}
	return view, base, routes, nil
}

func (s *Store) UnbindEntranceExit(id, exitID string) error {
	_, err := s.db.Exec("DELETE FROM entrance_exits WHERE entrance_id=? AND exit_id=?", id, exitID)
	return err
}
func (s *Store) DeleteEntrance(id string) error {
	result, err := s.db.Exec("DELETE FROM entrances WHERE id=?", id)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return domain.ErrNotFound
	}
	return nil
}
