package main

import (
	"strings"
	"testing"
)

func approved(diff string, acc ...bool) ReviewState {
	r := ReviewState{Status: "APPROVED", DiffArtifactID: diff}
	for _, a := range acc {
		r.AcceptanceResults = append(r.AcceptanceResults, struct{ Satisfied bool }{a})
	}
	return r
}

func TestDoneGateAllGreen(t *testing.T) {
	ok, why := doneGate(EvidenceOutcome{"PASS"}, EvidenceOutcome{"PASS"}, approved("art-1", true, true), "art-1")
	if !ok {
		t.Fatalf("expected pass, got %q", why)
	}
}

func TestDoneGateFailsOnEachCondition(t *testing.T) {
	cases := []struct {
		name  string
		b, tt EvidenceOutcome
		rv    ReviewState
		diff  string
		want  string
	}{
		{"build fail", EvidenceOutcome{"FAIL"}, EvidenceOutcome{"PASS"}, approved("a", true), "a", "build"},
		{"test fail", EvidenceOutcome{"PASS"}, EvidenceOutcome{"FAIL"}, approved("a", true), "a", "test"},
		{"not approved", EvidenceOutcome{"PASS"}, EvidenceOutcome{"PASS"}, ReviewState{Status: "CHANGES_REQUESTED", DiffArtifactID: "a"}, "a", "review"},
		{"wrong diff", EvidenceOutcome{"PASS"}, EvidenceOutcome{"PASS"}, approved("other", true), "a", "diff"},
		{"acceptance unsatisfied", EvidenceOutcome{"PASS"}, EvidenceOutcome{"PASS"}, approved("a", true, false), "a", "acceptance"},
	}
	for _, c := range cases {
		ok, why := doneGate(c.b, c.tt, c.rv, c.diff)
		if ok || !strings.Contains(why, c.want) {
			t.Fatalf("%s: ok=%v why=%q want substr %q", c.name, ok, why, c.want)
		}
	}
}

func TestDoneGateRequiresAcceptanceResults(t *testing.T) {
	ok, why := doneGate(EvidenceOutcome{"PASS"}, EvidenceOutcome{"PASS"}, approved("a"), "a")
	if ok || !strings.Contains(why, "acceptance") {
		t.Fatalf("ok=%v why=%q", ok, why)
	}
}
