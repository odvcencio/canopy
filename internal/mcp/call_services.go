package mcp

import (
	"fmt"

	"github.com/odvcencio/canopy/internal/federation"
)

func (s *Service) callServices(args map[string]any) (any, error) {
	dir, err := requiredStringArg(args, "federation")
	if err != nil {
		return nil, fmt.Errorf("federation directory is required: %w", err)
	}

	fi, err := federation.Load(dir)
	if err != nil {
		return nil, err
	}

	report := federation.BuildServiceGraph(fi)
	return report, nil
}
