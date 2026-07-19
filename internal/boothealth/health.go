// Package boothealth contains the pure decision contract used by the boot
// healthcheck and its tests. It does not perform service or network actions.
package boothealth

type Status string

const (
	StatusOK       Status = "ok"
	StatusDegraded Status = "degraded"
	StatusRollback Status = "rollback_required"
)

type Check struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
	Fatal   bool   `json:"fatal"`
}

type Report struct {
	Status          Status   `json:"status"`
	CheckedAt       string   `json:"checked_at"`
	DurationMS      int64    `json:"duration_ms"`
	Checks          []Check  `json:"checks"`
	FatalReasons    []string `json:"fatal_reasons"`
	DegradedReasons []string `json:"degraded_reasons"`
}

func Aggregate(checks []Check) (Status, []string, []string) {
	status := StatusOK
	var fatalReasons, degradedReasons []string
	for _, check := range checks {
		if check.Fatal || check.Status == "fatal" {
			status = StatusRollback
			fatalReasons = append(fatalReasons, check.Name)
			continue
		}
		if check.Status == "degraded" || check.Status == "failed" {
			if status == StatusOK {
				status = StatusDegraded
			}
			degradedReasons = append(degradedReasons, check.Name)
		}
	}
	return status, fatalReasons, degradedReasons
}

func ExitCode(status Status) int {
	if status == StatusRollback {
		return 1
	}
	return 0
}
