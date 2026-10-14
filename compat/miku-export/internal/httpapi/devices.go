package httpapi

import (
	"errors"
	"net/http"
	"time"

	"mikugate/internal/domain"
	"mikugate/internal/ingress"
)

func (a *Server) managedDevices(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	m, _, ok, revision, err := a.store.ManagedSnapshot(r.PathValue("id"))
	if err != nil {
		a.runtimeError(w, err)
		return
	}
	if !ok {
		fail(w, 404, "ingress_not_found", "尚未保存入口")
		return
	}
	write(w, 200, map[string]any{"devices": m.DeviceViews(), "legacy_enabled": !m.LegacyDisabled, "revision": revision, "max_devices": ingress.MaxDevices})
}

func (a *Server) deviceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ingress.ErrDeviceUnavailable):
		fail(w, 404, "device_unavailable", err.Error())
	case errors.Is(err, ingress.ErrDeviceCapacity):
		fail(w, 409, "device_capacity", err.Error())
	case errors.Is(err, domain.ErrConflict):
		fail(w, 409, "device_preview_changed", "入口或设备已变化，请刷新后再操作")
	default:
		a.runtimeError(w, err)
	}
}

func (a *Server) managedDeviceChange(mode string) func(http.ResponseWriter, *http.Request, domain.Session) {
	return a.stopped(func(w http.ResponseWriter, r *http.Request, _ domain.Session) {
		var input struct {
			Name     string `json:"name"`
			Revision string `json:"revision"`
			Confirm  bool   `json:"confirm"`
		}
		if !decode(w, r, &input) {
			return
		}
		if !input.Confirm {
			fail(w, 400, "confirm_required", "请确认更新设备授权；入口和出口须已停止，保存后手动启动")
			return
		}
		id := r.PathValue("id")
		material, _, ok, revision, err := a.store.ManagedSnapshot(id)
		if err != nil {
			a.runtimeError(w, err)
			return
		}
		if !ok {
			fail(w, 404, "ingress_not_found", "尚未保存入口")
			return
		}
		if input.Revision == "" || input.Revision != revision {
			a.deviceError(w, domain.ErrConflict)
			return
		}
		var changed *ingress.DeviceView
		switch mode {
		case "create":
			if len(material.Devices) >= ingress.MaxDevices {
				a.deviceError(w, ingress.ErrDeviceCapacity)
				return
			}
			device, err := ingress.NewDevice(input.Name)
			if err != nil {
				fail(w, 400, "invalid_device", "设备名称需为1–48个字符，且不能含控制字符")
				return
			}
			material.Devices = append(material.Devices, device)
			view := device.View()
			changed = &view
		case "legacy-revoke":
			material.LegacyDisabled = true
		case "revoke", "delete":
			deviceID := r.PathValue("device")
			found := false
			for n := range material.Devices {
				d := &material.Devices[n]
				if d.ID != deviceID {
					continue
				}
				found = true
				if mode == "delete" {
					if d.RevokedAt == nil {
						fail(w, 409, "device_active", "请先撤销设备，再移除记录")
						return
					}
					material.Devices = append(material.Devices[:n], material.Devices[n+1:]...)
				} else {
					if d.RevokedAt == nil {
						now := time.Now().UTC()
						d.RevokedAt = &now
					}
					view := d.View()
					changed = &view
				}
				break
			}
			if !found {
				a.deviceError(w, ingress.ErrDeviceUnavailable)
				return
			}
		}
		_, hash, err := a.store.ExitForRuntime(id)
		if err != nil {
			a.runtimeError(w, err)
			return
		}
		if err = a.store.ReplaceManagedClients(r.Context(), id, material, hash, revision); err != nil {
			a.deviceError(w, err)
			return
		}
		// Structural, vault and CAS checks are complete. Actual core validation
		// is performed on the next explicit start; no network probe is implied.
		write(w, 200, map[string]any{"device": changed, "legacy_enabled": !material.LegacyDisabled, "core_validated": false, "runtime_started": false, "resume_ingress": false, "restart_required": true})
	})
}

func (a *Server) managedDeviceClient(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	if !subscriptionConfirm(w, r) {
		return
	}
	id := r.PathValue("id")
	m, _, ok, err := a.store.Managed(id)
	if err != nil {
		a.runtimeError(w, err)
		return
	}
	if !ok {
		fail(w, 404, "ingress_not_found", "尚未保存入口")
		return
	}
	bundle, err := m.ClientBundleForDevice(r.PathValue("device"))
	if err != nil {
		a.deviceError(w, err)
		return
	}
	if err = a.store.AddEvent("info", "ingress.device_revealed", "管理员明确显示独立设备客户端材料，未返回其他设备或服务端秘密", id); err != nil {
		a.runtimeError(w, err)
		return
	}
	write(w, 200, map[string]any{"client_config": string(bundle.ClientJSON), "uri": bundle.URI, "core": m.Request.Core, "server_private_key_returned": false, "proxy_password_returned": false})
}
