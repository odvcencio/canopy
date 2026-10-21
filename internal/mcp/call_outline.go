package mcp

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"m31labs.dev/canopy/internal/outline"
)

func (s *Service) callOutline(args map[string]any) (any, error) {
	file, ok := args["file"].(string)
	if !ok || file == "" {
		return nil, fmt.Errorf("file is required")
	}
	root, err := filepath.Abs(s.defaultRoot)
	if err != nil {
		return nil, err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	target := file
	if !filepath.IsAbs(target) {
		target = filepath.Join(root, target)
	}
	target, err = filepath.EvalSymlinks(target)
	if err != nil {
		return nil, err
	}
	relative, err := filepath.Rel(root, target)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("outline file must be inside the server workspace")
	}
	cursor, _ := args["cursor"].(string)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	report, err := outline.ReadFile(ctx, target, outline.Options{Limit: intArg(args, "limit", outline.DefaultLimit), Cursor: cursor})
	if err != nil {
		return nil, err
	}
	report.Path = relative
	return report, nil
}
