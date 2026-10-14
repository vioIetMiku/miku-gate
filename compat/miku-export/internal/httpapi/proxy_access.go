package httpapi

import (
	"net"
	"net/http"
	"net/url"
	"strconv"

	"mikugate/internal/certassets"
	"mikugate/internal/domain"
)

func (a *Server) proxyAccess(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	var input struct {
		Public  bool `json:"public"`
		Confirm bool `json:"confirm"`
	}
	if !decode(w, r, &input) {
		return
	}
	if !input.Confirm {
		fail(w, 400, "confirm_required", "请确认修改认证代理监听范围")
		return
	}
	if input.Public && !a.allowPublicProxy {
		fail(w, 403, "public_proxy_disabled", "启动服务时需授权--allow-public-proxy")
		return
	}
	if err := a.store.SetProxyPublic(r.PathValue("id"), input.Public); err != nil {
		a.storeError(w, err)
		return
	}
	w.WriteHeader(204)
}

func (a *Server) proxyClient(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	var input struct {
		Confirm bool   `json:"confirm"`
		Address string `json:"address"`
	}
	if !decode(w, r, &input) {
		return
	}
	if !input.Confirm {
		fail(w, 400, "confirm_required", "连接地址包含代理密码，请确认显示")
		return
	}
	exit, _, err := a.store.ExitForRuntime(r.PathValue("id"))
	if err != nil {
		a.storeError(w, err)
		return
	}
	host := input.Address
	if host == "" && a.publicEndpoint != nil {
		host = a.publicEndpoint.Hostname
	}
	if host == "" {
		host = "127.0.0.1"
	}
	if ip := net.ParseIP(host); ip != nil {
		if ip.To4() == nil || ip.IsUnspecified() || ip.IsMulticast() {
			fail(w, 400, "address_invalid", "请输入VPS IPv4或域名")
			return
		}
		host = ip.String()
	} else {
		host, err = certassets.Hostname(host)
		if err != nil {
			fail(w, 400, "address_invalid", "连接地址需为域名或IPv4，不能是URL")
			return
		}
	}
	if exit.Listen == "127.0.0.1" {
		host = "127.0.0.1"
	}
	password, err := a.store.ProxyPassword(exit.ID)
	if err != nil {
		fail(w, 409, "proxy_credentials_unavailable", err.Error())
		return
	}
	uri := url.URL{Host: net.JoinHostPort(host, strconv.Itoa(exit.Port)), User: url.UserPassword(exit.Username, password)}
	uri.Scheme = "socks5"
	socks := uri.String()
	uri.Scheme = "http"
	state := any(nil)
	if a.gateway != nil {
		if runtime, ok := a.gateway.Snapshot(exit.ID); ok {
			state = runtime
		}
	}
	a.store.AddEvent("info", "exit.proxy_client_revealed", "管理员显示认证代理连接地址；日志不包含密码", exit.ID)
	write(w, 200, map[string]any{"socks5": socks, "http": uri.String(), "username": exit.Username, "password": password, "host": host, "port": exit.Port, "public": exit.Listen == "0.0.0.0", "runtime": state})
}
