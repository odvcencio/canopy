package mcp

import (
	"context"

	"m31labs.dev/canopy/pkg/changeintel"
)

// callReview is a thin adapter over pkg/changeintel.Service: it parses tool
// args into a changeintel.Request, calls Analyze, and returns the resulting
// Receipt directly. All change-intelligence logic lives in pkg/changeintel.
func (s *Service) callReview(args map[string]any) (any, error) {
	base, err := requiredStringArg(args, "base")
	if err != nil {
		return nil, err
	}

	target := s.stringArgOrDefault(args, "path", s.defaultRoot)
	head := stringArg(args, "head")
	policyVersion := stringArg(args, "policy_version")
	includeTests := boolArg(args, "include_tests", false)
	includeRisk := boolArg(args, "include_risk", false)

	svc := changeintel.NewService()
	receipt, err := svc.Analyze(context.Background(), changeintel.Request{
		Root:          target,
		Base:          changeintel.SnapshotRef{Ref: base},
		Head:          changeintel.SnapshotRef{Ref: head},
		IncludeTests:  includeTests,
		IncludeRisk:   includeRisk,
		PolicyVersion: policyVersion,
	})
	if err != nil {
		return nil, err
	}
	return receipt, nil
}
