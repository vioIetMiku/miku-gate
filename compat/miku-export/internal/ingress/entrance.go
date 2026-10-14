package ingress

import (
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
)

// An entrance owns one listener and one TLS/REALITY identity. Each landing has
// independent client credentials and a fixed authenticated local VPN proxy.
type LandingRoute struct {
	ExitID   string          `json:"exit_id"`
	Name     string          `json:"name"`
	Identity ManagedIdentity `json:"identity"`
	Proxy    Proxy           `json:"proxy"`
}

func routeIdentity(base ManagedMaterial, identity ManagedIdentity) ManagedIdentity {
	identity.RealityPrivateKey, identity.ShortID = base.Identity.RealityPrivateKey, base.Identity.ShortID
	return identity
}

func LandingClient(base ManagedMaterial, route LandingRoute) (ClientBundle, error) {
	material := base
	material.Identity = routeIdentity(base, route.Identity)
	material.Proxy = route.Proxy
	material.Devices, material.LegacyDisabled, material.CoreRollback = nil, false, nil
	bundle, err := material.ClientBundle()
	if err != nil {
		return bundle, err
	}
	u, err := url.Parse(bundle.URI)
	if err != nil {
		return ClientBundle{}, err
	}
	u.Fragment = route.Name
	bundle.URI = u.String()
	return bundle, nil
}

// RenderLandings shares a listener while routing by authenticated user. No
// default/freedom/direct outbound exists, and a stopped landing cannot fall
// through to another country or the VPS's own internet connection.
func RenderLandings(base ManagedMaterial, routes []LandingRoute) ([]byte, error) {
	if len(routes) < 1 || len(routes) > 32 {
		return nil, errors.New("入口需绑定1–32个落地")
	}
	base.Devices, base.LegacyDisabled, base.CoreRollback = nil, false, nil
	bundle, err := base.Render()
	if err != nil {
		return nil, err
	}
	config, err := parseJSON(bundle.ServerJSON)
	if err != nil {
		return nil, err
	}
	inbound := object(array(config["inbounds"])[0])
	users, outbounds, rules := []any{}, []any{}, []any{}
	ids, credentials := map[string]bool{}, map[string]bool{}
	if base.Request.Core == "xray" {
		outbounds = append(outbounds, map[string]any{"tag": "block", "protocol": "blackhole", "settings": map[string]any{}})
		rules = append(rules, map[string]any{"type": "field", "inboundTag": []string{"miku-managed-in"}, "network": "udp", "outboundTag": "block"})
	} else {
		rules = append(rules, map[string]any{"network": "udp", "action": "reject", "method": "drop"})
	}
	for _, route := range routes {
		identity := routeIdentity(base, route.Identity)
		if !regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`).MatchString(route.ExitID) || ids[route.ExitID] || credentials[identity.UUID] || identity.Validate(base.Request.Template == "vless-reality-vision") != nil || route.Proxy.Validate() != nil || route.Proxy.Port == base.Request.Port {
			return nil, ErrConfig
		}
		ids[route.ExitID], credentials[identity.UUID] = true, true
		user, tag := "landing-"+route.ExitID+"@miku.invalid", "landing-"+route.ExitID
		if base.Request.Core == "xray" {
			client := map[string]any{"email": user}
			if base.Request.Template == "trojan-tls" {
				client["password"] = identity.Password
			} else {
				client["id"], client["flow"] = identity.UUID, "xtls-rprx-vision"
			}
			users = append(users, client)
			outbounds = append(outbounds, map[string]any{"tag": tag, "protocol": "socks", "settings": map[string]any{"servers": []any{map[string]any{"address": "127.0.0.1", "port": route.Proxy.Port, "users": []any{map[string]any{"user": route.Proxy.Username, "pass": route.Proxy.Password}}}}}})
			rules = append(rules, map[string]any{"type": "field", "inboundTag": []string{"miku-managed-in"}, "user": []string{user}, "network": "tcp", "outboundTag": tag})
		} else {
			client := map[string]any{"name": user}
			if base.Request.Template == "trojan-tls" {
				client["password"] = identity.Password
			} else {
				client["uuid"], client["flow"] = identity.UUID, "xtls-rprx-vision"
			}
			users = append(users, client)
			outbounds = append(outbounds, map[string]any{"tag": tag, "type": "socks", "server": "127.0.0.1", "server_port": route.Proxy.Port, "version": "5", "username": route.Proxy.Username, "password": route.Proxy.Password, "network": "tcp"})
			rules = append(rules, map[string]any{"inbound": []string{"miku-managed-in"}, "auth_user": []string{user}, "network": "tcp", "action": "route", "outbound": tag})
		}
	}
	if base.Request.Core == "xray" {
		object(inbound["settings"])["clients"] = users
		rules = append(rules, map[string]any{"type": "field", "network": "tcp,udp", "outboundTag": "block"})
		config["routing"] = map[string]any{"domainStrategy": "AsIs", "rules": rules}
	} else {
		inbound["users"] = users
		rules = append(rules, map[string]any{"action": "reject", "method": "drop"})
		config["route"] = map[string]any{"rules": rules}
	}
	config["outbounds"] = outbounds
	raw, err := json.Marshal(config)
	if err != nil || len(raw) > MaxConfig {
		return nil, ErrConfig
	}
	return append(raw, '\n'), nil
}
