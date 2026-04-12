package mcp

import (
	"fmt"

	"github.com/odvcencio/canopy/pkg/query"
	"github.com/odvcencio/canopy/pkg/refactor"
)

func (s *Service) callRefactor(args map[string]any) (any, error) {
	selectorRaw, err := requiredStringArg(args, "selector")
	if err != nil {
		return nil, err
	}
	newName, err := requiredStringArg(args, "new_name")
	if err != nil {
		return nil, err
	}
	engine := s.stringArgOrDefault(args, "engine", "go")
	updateCallsites := boolArg(args, "callsites", false)
	crossPackage := boolArg(args, "cross_package", false)
	writeChanges := boolArg(args, "write", false)
	if writeChanges && !s.allowWrites {
		return nil, fmt.Errorf("write operations are disabled for this MCP server")
	}
	if crossPackage && !updateCallsites {
		return nil, fmt.Errorf("cross_package requires callsites=true")
	}

	target := s.stringArgOrDefault(args, "path", s.defaultRoot)
	cachePath := s.stringArgOrDefault(args, "cache", s.defaultCache)
	idx, err := s.loadOrBuild(cachePath, target)
	if err != nil {
		return nil, err
	}
	idx = applyGeneratedFilter(idx, boolArg(args, "include_generated", false), stringArg(args, "generator"))

	selector, err := query.ParseSelector(selectorRaw)
	if err != nil {
		return nil, err
	}
	report, err := refactor.RenameDeclarations(idx, selector, newName, refactor.Options{
		Write:                 writeChanges,
		UpdateCallsites:       updateCallsites,
		CrossPackageCallsites: crossPackage,
		Engine:                engine,
	})
	if err != nil {
		return nil, err
	}
	return report, nil
}
