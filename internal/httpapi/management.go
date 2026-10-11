package httpapi

import (
	"mikugate/internal/domain"
	"net/http"
)

func (a *Server) managementAccess(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	mode, origin := "loopback-http", "http://"+r.Host
	if r.TLS != nil {
		mode, origin = "loopback-https", "https://"+r.Host
	}
	configured := a.publicEndpoint != nil
	if configured {
		mode, origin = "explicit-direct-https", a.publicEndpoint.Origin
	}
	response := map[string]any{"mode": mode, "origin": origin, "tls_active": r.TLS != nil, "explicit_origin_configured": configured, "forwarding_headers_trusted": false, "private_key_returned": false, "certificate_status": "unknown"}
	if a.managementIdentity != nil && a.managementIdentity.Origin == origin {
		status := "verified-system-chain"
		if a.managementIdentity.Bootstrap {
			status = "explicit-ip-bootstrap-not-public-ca"
		}
		response["certificate_status"] = status
		response["certificate"] = a.managementIdentity
	}
	write(w, 200, response)
}
