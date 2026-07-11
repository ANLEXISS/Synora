package api

// Server carries the minimal web configuration used by the static web handler.
// The HTTP API server in cmd/synora-api has its own router; this package is
// kept buildable for the web-serving path used by embedded/front-end consumers.
type Server struct {
	WebEnabled bool
	WebRoot    string
}
