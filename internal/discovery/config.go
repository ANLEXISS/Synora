package discovery

const (
	VisionClipDir   = "/var/lib/synora/clips"
	HealthAddr      = ":8091"
	VisionHTTPSAddr = ":7070"

	MaxClipSize = 50 << 20

	CertFile = "/etc/synora/certs/server.crt"
	KeyFile  = "/etc/synora/certs/server.key"
)