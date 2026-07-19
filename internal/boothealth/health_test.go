package boothealth

import "testing"

func TestAggregateAndExitCodes(t *testing.T) {
	status, fatal, degraded := Aggregate([]Check{{Name: "weapon", Status: "degraded"}})
	if status != StatusDegraded || len(fatal) != 0 || len(degraded) != 1 || ExitCode(status) != 0 {
		t.Fatalf("unexpected degraded result: %v %v %v", status, fatal, degraded)
	}
	status, fatal, _ = Aggregate([]Check{{Name: "api", Status: "fatal"}})
	if status != StatusRollback || len(fatal) != 1 || ExitCode(status) != 1 {
		t.Fatalf("unexpected rollback result: %v %v", status, fatal)
	}
	status, fatal, _ = Aggregate([]Check{{Name: "synoranet", Status: "degraded", Fatal: true}})
	if status != StatusRollback || len(fatal) != 1 {
		t.Fatalf("fatal degraded check did not roll back: %v %v", status, fatal)
	}
}
