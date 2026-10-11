package ingress

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"strings"

	"mikugate/internal/certassets"
	"mikugate/internal/egress"
)

// Recoverable secrets ONLY for local vault persistence. Never serialize this
// object into an HTTP response: use ManagedBundle metadata or ManagedView.
type ManagedIdentity struct {
	UUID              string `json:"uuid"`
	Password          string `json:"password"`
	RealityPrivateKey string `json:"reality_private_key"`
	ShortID           string `json:"short_id"`
}

func (i ManagedIdentity) Validate(reality bool) error {
	if !regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`).MatchString(i.UUID) || len(i.Password) != 32 || strings.ContainsAny(i.Password, "\x00\r\n") {
		return ErrConfig
	}
	if reality {
		key, e := base64.RawURLEncoding.DecodeString(i.RealityPrivateKey)
		if e != nil || len(key) != 32 {
			return ErrConfig
		}
		sid, e := hex.DecodeString(i.ShortID)
		if e != nil || len(sid) != 8 {
			return ErrConfig
		}
	} else if i.RealityPrivateKey != "" || i.ShortID != "" {
		return ErrConfig
	}
	return nil
}

type ManagedMaterial struct {
	Request        ManagedRequest  `json:"request"`
	Proxy          Proxy           `json:"proxy"`
	Identity       ManagedIdentity `json:"identity"`
	CertificatePEM []byte          `json:"certificate_pem,omitempty"`
	PrivateKeyPEM  []byte          `json:"private_key_pem,omitempty"`
	Target         *RealityTarget  `json:"target,omitempty"`
}

// Render reconstructs only generated templates; persistence never accepts an
// arbitrary program/config path or raw core JSON. Wrong/expired assets fail.
func (m ManagedMaterial) Render() (ManagedBundle, error) {
	plan, e := ManagedPlanFor(m.Request)
	if e != nil || m.Identity.Validate(plan.NeedsRealityTarget) != nil || m.Proxy.Validate() != nil {
		return ManagedBundle{}, ErrConfig
	}
	var asset *certassets.Asset
	var target *RealityTarget
	if plan.NeedsCertificate {
		if m.Target != nil {
			return ManagedBundle{}, ErrConfig
		}
		asset, e = certassets.Validate(plan.Request.ServerName, m.CertificatePEM, m.PrivateKeyPEM)
		if e != nil {
			return ManagedBundle{}, e
		}
	} else {
		if m.Target == nil || len(m.CertificatePEM) != 0 || len(m.PrivateKeyPEM) != 0 || egress.PublicIPv4(m.Target.Address) != nil || m.Target.Port != 443 {
			return ManagedBundle{}, ErrReality
		}
		copy := *m.Target
		copy.verified = true
		target = &copy
	}
	bundle, e := generateManaged(m.Request, m.Proxy, asset, ManagedFiles{"/miku-inline/certificate.pem", "/miku-inline/private.key"}, target, &m.Identity)
	if e != nil {
		return ManagedBundle{}, e
	}
	if asset != nil {
		config, e := parseJSON(bundle.ServerJSON)
		if e != nil {
			return ManagedBundle{}, e
		}
		in := object(array(config["inbounds"])[0])
		certLines := strings.Split(strings.TrimSpace(string(m.CertificatePEM)), "\n")
		keyLines := strings.Split(strings.TrimSpace(string(m.PrivateKeyPEM)), "\n")
		if m.Request.Core == "xray" {
			tls := object(object(in["streamSettings"])["tlsSettings"])
			tls["certificates"] = []any{map[string]any{"certificate": certLines, "key": keyLines, "oneTimeLoading": true}}
		} else {
			tls := object(in["tls"])
			delete(tls, "certificate_path")
			delete(tls, "key_path")
			tls["certificate"] = certLines
			tls["key"] = keyLines
		}
		bundle.ServerJSON, e = json.Marshal(config)
		if e != nil {
			return ManagedBundle{}, ErrConfig
		}
		bundle.ServerJSON = append(bundle.ServerJSON, '\n')
		bundle.ServerSHA256 = digest(bundle.ServerJSON)
	}
	return bundle, nil
}
func (m ManagedMaterial) ValidateBinding(port int, username string) error {
	if m.Proxy.Port != port || m.Proxy.Username != username {
		return errors.New("入口认证目标与逻辑出口不匹配")
	}
	_, e := m.Render()
	return e
}
