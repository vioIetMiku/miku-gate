package proxy

import "sync"

// Snapshot counts this frontend's TCP connections and successful forwarding
// writes. It retains no destinations, usernames, credentials or payloads.
type Snapshot struct {
	UDPAssociations      int    `json:"udp_associations"`
	UDPDatagramsUpload   uint64 `json:"udp_upload_datagrams"`
	UDPDatagramsDownload uint64 `json:"udp_download_datagrams"`
	UDPUploadBytes       uint64 `json:"udp_upload_bytes"`
	UDPDownloadBytes     uint64 `json:"udp_download_bytes"`
	AcceptedConnections  uint64 `json:"accepted_connections"`
	ActiveConnections    int    `json:"active_connections"`
	Authenticated        uint64 `json:"authenticated_connections"`
	EstablishedStreams   uint64 `json:"established_streams"`
	ActiveStreams        int    `json:"active_streams"`
	AuthenticationFails  uint64 `json:"authentication_failures"`
	DialFailures         uint64 `json:"dial_failures"`
	CapacityRejected     uint64 `json:"capacity_rejected"`
	UploadBytes          uint64 `json:"upload_bytes"`
	DownloadBytes        uint64 `json:"download_bytes"`
}

type Metrics struct {
	mu sync.Mutex
	v  Snapshot
}

func (m *Metrics) update(fn func(*Snapshot)) {
	if m != nil {
		m.mu.Lock()
		defer m.mu.Unlock()
		fn(&m.v)
	}
}
func (m *Metrics) Snapshot() Snapshot {
	if m == nil {
		return Snapshot{}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.v
}
func (m *Metrics) accepted() {
	m.update(func(v *Snapshot) { v.AcceptedConnections++; v.ActiveConnections++ })
}
func (m *Metrics) closed() { m.update(func(v *Snapshot) { v.ActiveConnections-- }) }
func (m *Metrics) rejected() {
	m.update(func(v *Snapshot) { v.AcceptedConnections++; v.CapacityRejected++ })
}
func (m *Metrics) authenticated() { m.update(func(v *Snapshot) { v.Authenticated++ }) }
func (m *Metrics) authFailed()    { m.update(func(v *Snapshot) { v.AuthenticationFails++ }) }
func (m *Metrics) dialFailed()    { m.update(func(v *Snapshot) { v.DialFailures++ }) }
func (m *Metrics) established() {
	m.update(func(v *Snapshot) { v.EstablishedStreams++; v.ActiveStreams++ })
}
func (m *Metrics) streamClosed() { m.update(func(v *Snapshot) { v.ActiveStreams-- }) }
func (m *Metrics) upload(n int) {
	if n > 0 {
		m.update(func(v *Snapshot) { v.UploadBytes += uint64(n) })
	}
}
func (m *Metrics) download(n int) {
	if n > 0 {
		m.update(func(v *Snapshot) { v.DownloadBytes += uint64(n) })
	}
}

type countingWriter struct {
	writer interface{ Write([]byte) (int, error) }
	count  func(int)
}

func (w countingWriter) Write(raw []byte) (int, error) {
	n, err := w.writer.Write(raw)
	w.count(n)
	return n, err
}

func (m *Metrics) udpStarted() { m.update(func(v *Snapshot) { v.UDPAssociations++ }) }
func (m *Metrics) udpClosed()  { m.update(func(v *Snapshot) { v.UDPAssociations-- }) }
func (m *Metrics) udpUpload(n int) {
	m.update(func(v *Snapshot) { v.UDPDatagramsUpload++; v.UDPUploadBytes += uint64(n) })
}
func (m *Metrics) udpDownload(n int) {
	m.update(func(v *Snapshot) { v.UDPDatagramsDownload++; v.UDPDownloadBytes += uint64(n) })
}
