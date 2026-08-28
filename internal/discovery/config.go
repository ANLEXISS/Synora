package discovery

import "synora/internal/runtimeconfig"

const (
	VisionClipDir   = runtimeconfig.DefaultClipRoot
	HealthAddr      = runtimeconfig.DefaultVisionHealth
	VisionHTTPSAddr = runtimeconfig.DefaultVisionHTTPS

	MaxClipSize = 50 << 20

	CertFile = runtimeconfig.DefaultConfigDir + "/tls/synora.crt"
	KeyFile  = runtimeconfig.DefaultConfigDir + "/tls/synora.key"
)
