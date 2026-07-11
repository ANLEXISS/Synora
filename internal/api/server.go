package api

// Server carries the minimal web configuration used by the static web handler.
// The HTTP API server in cmd/synora-api has its own router; this package is
// kept buildable for the web-serving path used by embedded/front-end consumers.
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
