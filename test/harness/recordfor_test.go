package harness

import "testing"

// The signal strategy journals the service as the controller derived it (compose label,
// else the container ID); restart and rolling journal the container name. RecordFor must
// find both, or journal-dependent invariants silently skip signal rotations.

func TestRecordFor_FindsARecordKeyedByTheServiceContainersID(t *testing.T) {
	s := Snapshot{
		Containers: []CView{{ID: "ctr-id-1", Name: "svc", Owner: owner, Running: true}, {ID: "other", Name: "unrelated", Owner: owner}},
		Journal:    []RotationRecord{rec("intent_signal_publish", "critical_error", func(r *RotationRecord) { r.ServiceID = "ctr-id-1"; r.Strategy = "signal" })},
	}
	r, ok := s.RecordFor("svc")
	if !ok || r.Status != "critical_error" || r.Strategy != "signal" {
		t.Fatalf("a record keyed by the service container's ID must be found, got %+v found=%v", r, ok)
	}
}

func TestRecordFor_ByNameStillWorksAndDoesNotCrossServices(t *testing.T) {
	s := Snapshot{
		Containers: []CView{{ID: "id-a", Name: "svc-a", Owner: owner}, {ID: "id-b", Name: "svc-b", Owner: owner}},
		Journal: []RotationRecord{
			rec("intent_swap", "in_progress", func(r *RotationRecord) { r.ServiceID = "svc-a" }),
			rec("intent_signal_publish", "critical_error", func(r *RotationRecord) { r.ServiceID = "id-b" }),
		},
	}
	if r, ok := s.RecordFor("svc-a"); !ok || r.Transition != "intent_swap" {
		t.Fatalf("a name-keyed record must still be found for its service, got %+v %v", r, ok)
	}
	if r, ok := s.RecordFor("svc-b"); !ok || r.Transition != "intent_signal_publish" {
		t.Fatalf("an ID-keyed record must be found only for its own service, got %+v %v", r, ok)
	}
	if _, ok := (Snapshot{Containers: s.Containers, Journal: []RotationRecord{rec("x", "in_progress", func(r *RotationRecord) { r.ServiceID = "someone-elses-id" })}}).RecordFor("svc-a"); ok {
		t.Fatal("a record whose service_id matches no container of the service must not be returned")
	}
}
