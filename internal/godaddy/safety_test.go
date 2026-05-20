package godaddy

import "testing"

func intPtr(v int) *int {
	return &v
}

func TestPlanDeleteOneRequiresData(t *testing.T) {
	_, err := PlanDeleteOne([]DNSRecord{{Data: "1.2.3.4"}}, DeleteSelector{})
	if err == nil {
		t.Fatal("PlanDeleteOne() error = nil, want error")
	}
}

func TestPlanDeleteOneRefusesAmbiguousMatch(t *testing.T) {
	records := []DNSRecord{
		{Type: "A", Name: "www", Data: "1.2.3.4", TTL: intPtr(600)},
		{Type: "A", Name: "www", Data: "1.2.3.4", TTL: intPtr(3600)},
	}

	_, err := PlanDeleteOne(records, DeleteSelector{Data: "1.2.3.4"})
	if err == nil {
		t.Fatal("PlanDeleteOne() error = nil, want ambiguity error")
	}
}

func TestPlanDeleteOneUsesRemainingRecords(t *testing.T) {
	records := []DNSRecord{
		{Type: "A", Name: "www", Data: "1.2.3.4", TTL: intPtr(600)},
		{Type: "A", Name: "www", Data: "5.6.7.8", TTL: intPtr(600)},
	}

	plan, err := PlanDeleteOne(records, DeleteSelector{Data: "1.2.3.4"})
	if err != nil {
		t.Fatalf("PlanDeleteOne() error = %v", err)
	}
	if plan.UseDirectDelete {
		t.Fatal("UseDirectDelete = true, want false")
	}
	if len(plan.Remaining) != 1 || plan.Remaining[0].Data != "5.6.7.8" {
		t.Fatalf("Remaining = %#v, want only 5.6.7.8", plan.Remaining)
	}
}

func TestPlanDeleteOneUsesDirectDeleteOnlyForSingleExistingRecord(t *testing.T) {
	records := []DNSRecord{{Type: "TXT", Name: "_acme-challenge", Data: "token"}}

	plan, err := PlanDeleteOne(records, DeleteSelector{Data: "token"})
	if err != nil {
		t.Fatalf("PlanDeleteOne() error = %v", err)
	}
	if !plan.UseDirectDelete {
		t.Fatal("UseDirectDelete = false, want true")
	}
	if len(plan.Remaining) != 0 {
		t.Fatalf("Remaining = %#v, want empty", plan.Remaining)
	}
}
