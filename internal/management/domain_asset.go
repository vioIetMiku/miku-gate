package management

import "mikugate/internal/certassets"

// DomainAsset reuses only the explicit management TLS files for the SAME DNS
// identity. No caller-controlled path or browser-supplied PEM is accepted. The
// installed ordinary control UID can read its private copy without opening root
// ACME files; root originals remain untouched and each reuse revalidates them.
func DomainAsset(endpoint Endpoint, certPath, keyPath, hostname string) (certassets.Discovery, *certassets.Asset, error) {
	checked, err := ParseOrigin(endpoint.Origin)
	if err != nil || checked != endpoint || endpoint.IP {
		return certassets.Discovery{}, nil, certassets.ErrCertificate
	}
	host, err := certassets.Hostname(hostname)
	if err != nil || host != endpoint.Hostname {
		return certassets.Discovery{}, nil, certassets.ErrCertificate
	}
	report := certassets.Discovery{Hostname: host, Findings: []certassets.Finding{{Provider: "management-domain-copy", State: "unreadable"}}}
	cert, err := readPEM(certPath, false)
	if err != nil {
		return report, nil, nil
	}
	key, err := readPEM(keyPath, true)
	if err != nil {
		return report, nil, nil
	}
	defer clear(key)
	asset, err := certassets.Validate(host, cert, key)
	if err != nil {
		report.Findings[0].State = "unverified"
		return report, nil, nil
	}
	summary := asset.Summary()
	report.Findings[0].State = "verified"
	report.Findings[0].Certificate = &summary
	report.SelectedProvider = "management-domain-copy"
	return report, asset, nil
}
