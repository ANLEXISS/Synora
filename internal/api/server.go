package api

// Server carries the minimal web configuration used by the static web handler.
// Discovery owns the HTTP API router; this package retains only shared web
// and authentication primitives used by local tools.
type Server struct {
	WebEnabled bool
	WebRoot    string
}

type ServerHealth struct {
	HTTPAddr       string `json:"http_addr"`
	HTTPSEnabled   bool   `json:"https_enabled"`
	HTTPSAddr      string `json:"https_addr"`
	TLSCertPresent bool   `json:"tls_cert_present"`
	TLSKeyPresent  bool   `json:"tls_key_present"`
}
