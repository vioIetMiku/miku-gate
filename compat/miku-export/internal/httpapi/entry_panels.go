package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"mikugate/internal/paneladapter"
	"net/http"
	"time"

	"mikugate/internal/domain"
	"mikugate/internal/entrypanel"
	"mikugate/internal/ingress"
)

type existingEntryInput struct {
	Kind       string `json:"kind"`
	Port       int    `json:"port"`
	Base       string `json:"base"`
	HTTPS      bool   `json:"https"`
	TLSName    string `json:"tls_name"`
	Username   string `json:"username"`
	Secret     string `json:"secret"`
	Tag        string `json:"tag"`
	Address    string `json:"address"`
	ServerName string `json:"server_name"`
	Name       string `json:"name"`
	Confirm    bool   `json:"confirm"`
}

func (input existingEntryInput) spec() ingress.ExternalEntrance {
	return ingress.ExternalEntrance{Kind: input.Kind, Port: input.Port, Base: input.Base, HTTPS: input.HTTPS, TLSName: input.TLSName, Username: input.Username, Secret: input.Secret}
}

func (a *Server) entryPanelInspect(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	var input existingEntryInput
	if !decode(w, r, &input) {
		return
	}
	if !input.Confirm {
		fail(w, 400, "confirm_required", "请确认读取你管理的本机面板")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 24*time.Second)
	defer cancel()
	client, err := entrypanel.New(ctx, input.spec())
	if err != nil {
		fail(w, 422, "panel_unavailable", err.Error())
		return
	}
	snapshot, err := client.Read(ctx)
	if err != nil {
		fail(w, 422, "panel_unavailable", err.Error())
		return
	}
	nodes, err := client.Inbounds(snapshot)
	if err != nil {
		fail(w, 422, "panel_unavailable", err.Error())
		return
	}
	write(w, 200, nodes)
}

func (a *Server) entryPanelAttach(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	var input existingEntryInput
	if !decode(w, r, &input) {
		return
	}
	if !input.Confirm {
		fail(w, 400, "confirm_required", "请确认复用入口，并将面板凭据在本机加密保存")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 24*time.Second)
	defer cancel()
	client, err := entrypanel.New(ctx, input.spec())
	if err != nil {
		fail(w, 422, "panel_unavailable", err.Error())
		return
	}
	snapshot, err := client.Read(ctx)
	if err != nil {
		fail(w, 422, "panel_unavailable", err.Error())
		return
	}
	material, err := client.Material(snapshot, input.Tag, input.Address, input.ServerName)
	if err != nil {
		fail(w, 422, "entry_unavailable", err.Error())
		return
	}
	name := input.Name
	if name == "" {
		name = input.Kind + " · " + input.Tag
	}
	view, err := a.store.CreateEntrance(name, input.Kind, material)
	if err != nil {
		a.storeError(w, err)
		return
	}
	write(w, 201, view)
}

func (a *Server) externalEntranceApply(ctx context.Context, id string, base ingress.ManagedMaterial, routes []ingress.LandingRoute, pause bool) error {
	client, err := entrypanel.New(ctx, *base.External)
	if err != nil {
		return err
	}
	if !pause && a.gateway == nil {
		return errors.New("未接入落地运行时，不能应用代理通路")
	}
	if !pause && a.gateway != nil {
		ready := false
		for _, route := range routes {
			if state, ok := a.gateway.Snapshot(route.ExitID); ok && state.Healthy && state.ProxyListening {
				ready = true
			}
		}
		if !ready {
			return errors.New("请先启动至少一个OpenVPN落地")
		}
	}
	before, err := client.Read(ctx)
	if err != nil {
		return err
	}
	routeBytes, _ := json.Marshal(routes)
	sum := sha256.Sum256(routeBytes)
	routeHash := hex.EncodeToString(sum[:])
	if base.External.AppliedSHA == paneladapter.Hash(before.Rendered) && base.External.RoutesSHA == routeHash && base.External.Paused == pause {
		return nil
	}
	instance, err := a.store.InstanceID()
	if err != nil {
		return err
	}
	private := panelBackup{CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), Initial: base.External.AppliedSHA == "", Instance: instance, EntranceID: id, PanelKind: base.External.Kind, Endpoint: paneladapter.Endpoint{Port: base.External.Port, Base: base.External.Base, Mode: "current", HTTPS: base.External.HTTPS, TLSName: base.External.TLSName}, Before: paneladapter.Snapshot{Template: before.Template, Rendered: before.Rendered}, Plan: ingress.Plan{InboundTag: base.External.InboundTag}}
	raw, err := json.Marshal(private)
	if err != nil {
		return err
	}
	ids, e := a.store.PanelBackupIDs()
	if e != nil {
		clear(raw)
		return e
	}
	if len(ids) >= 8 {
		oldestID, oldestTime := "", ""
		for _, checkpoint := range ids {
			data, e := a.store.PanelBackup(checkpoint)
			if e != nil {
				clear(raw)
				return e
			}
			var record panelBackup
			e = json.Unmarshal(data, &record)
			clear(data)
			if e == nil && record.EntranceID == id && record.PanelKind == base.External.Kind && !record.Initial && record.CreatedAt != "" && (oldestTime == "" || record.CreatedAt < oldestTime) {
				oldestID, oldestTime = checkpoint, record.CreatedAt
			}
		}
		if oldestID != "" {
			if e = a.store.ForgetPanelBackup(oldestID); e != nil {
				clear(raw)
				return e
			}
		}
	}
	backup, err := a.store.SavePanelBackup(raw)
	clear(raw)
	if err != nil {
		return err
	}
	a.store.AddEvent("info", "entrance.panel_checkpoint", "已有面板路由修改前已保存私有快照："+backup, "")
	if err = client.Apply(ctx, id, base, routes, pause); err != nil {
		return err
	}
	after, err := client.Read(ctx)
	if err != nil {
		return err
	}
	view, _, err := a.store.Entrance(id)
	if err != nil {
		return err
	}
	base.External.AppliedSHA, base.External.RoutesSHA, base.External.Paused = paneladapter.Hash(after.Rendered), routeHash, pause
	return a.store.UpdateEntrance(id, view.Name, view.Revision, base)
}
