package network

import "log"

func EnsureFirewall() error {
	// SynoraNet is intentionally isolated. Existing firewall/NAT policy is not
	// changed here; deployments may add an explicit allow rule for 8443/8554.
	log.Println("firewall policy unchanged; SynoraNet remains local-only")

	return nil
}
