package discovery

import (
	"fmt"
	"net/http"
)

func (m *Manager) VerifyCameraRequest(
	r *http.Request,
	bodyHash string,
) error {

	if m.auth == nil {
		return fmt.Errorf("device auth unavailable")
	}
	return m.auth.VerifyRequest(r, bodyHash)
}
