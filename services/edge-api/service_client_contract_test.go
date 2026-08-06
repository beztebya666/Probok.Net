package main

import (
	"testing"

	"github.com/greenroute/greenroute/internal/domain"
)

func TestValidateResultAcceptsLabelledBestEffortRoutes(t *testing.T) {
	result := validContractResult()
	result.BestEffortRoutes = []domain.RouteCandidate{contractCandidate("near-green", []string{
		"BEST_EFFORT_NOT_STRICT_GREEN", "STRICT_GREEN_NON_GREEN_SEGMENT",
	})}
	if err := validateResult(result); err != nil {
		t.Fatalf("valid best-effort contract rejected: %v", err)
	}
}

func TestValidateResultRejectsUnlabelledOrExcessBestEffortRoutes(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*domain.RouteSearchResult)
	}{
		{"missing arrays", func(result *domain.RouteSearchResult) { result.BestEffortRoutes = nil }},
		{"unlabelled", func(result *domain.RouteSearchResult) {
			result.BestEffortRoutes = []domain.RouteCandidate{contractCandidate("candidate", []string{"PROVIDER_ROUTE_DETAILS"})}
		}},
		{"too many", func(result *domain.RouteSearchResult) {
			for index := 0; index < 4; index++ {
				result.BestEffortRoutes = append(result.BestEffortRoutes, contractCandidate(string(rune('a'+index)), []string{"BEST_EFFORT_NOT_STRICT_GREEN", "STRICT_GREEN_NON_GREEN_SEGMENT"}))
			}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := validContractResult()
			test.mutate(&result)
			if err := validateResult(result); err == nil {
				t.Fatal("invalid best-effort contract was accepted")
			}
		})
	}
}

func validContractResult() domain.RouteSearchResult {
	return domain.RouteSearchResult{
		SearchID: "search", Status: domain.SearchDegraded,
		Alternatives: []domain.RouteCandidate{}, BestEffortRoutes: []domain.RouteCandidate{},
	}
}

func contractCandidate(id string, reasons []string) domain.RouteCandidate {
	return domain.RouteCandidate{
		CandidateID: id, Confidence: domain.Confidence{Level: domain.ConfidenceMedium, Score: .65},
		ReasonCodes: reasons,
	}
}
