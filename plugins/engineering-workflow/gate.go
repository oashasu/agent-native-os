package main

import "fmt"

type EvidenceOutcome struct{ Outcome string }

type ReviewState struct {
	Status            string
	DiffArtifactID    string
	AcceptanceResults []struct{ Satisfied bool }
}

// doneGate evaluates the locked M1 §4.3 conjunction. The first failure wins.
func doneGate(build, test EvidenceOutcome, review ReviewState, currentDiffArtifactID string) (bool, string) {
	if build.Outcome != "PASS" {
		return false, fmt.Sprintf("build: outcome %s", build.Outcome)
	}
	if test.Outcome != "PASS" {
		return false, fmt.Sprintf("test: outcome %s", test.Outcome)
	}
	if review.Status != "APPROVED" {
		return false, fmt.Sprintf("review: status %s", review.Status)
	}
	if review.DiffArtifactID != currentDiffArtifactID {
		return false, fmt.Sprintf("diff: reviewed %s != current %s", review.DiffArtifactID, currentDiffArtifactID)
	}
	if len(review.AcceptanceResults) == 0 {
		return false, "acceptance: no results"
	}
	for _, result := range review.AcceptanceResults {
		if !result.Satisfied {
			return false, "acceptance: unsatisfied result"
		}
	}
	return true, ""
}
