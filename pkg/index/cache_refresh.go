package index

import (
	"context"
	"fmt"
	"strings"

	"m31labs.dev/canopy/pkg/model"
)

// CacheRefreshStatus describes how EnsureFreshCache used the cached index.
type CacheRefreshStatus string

const (
	CacheUnchanged              CacheRefreshStatus = "unchanged"
	CacheIncrementallyRefreshed CacheRefreshStatus = "incrementally_refreshed"
	CacheFullyRebuilt           CacheRefreshStatus = "fully_rebuilt"
)

// EnsureFreshCache validates an auto-discovered cache and refreshes it when required.
func (b *Builder) EnsureFreshCache(
	ctx context.Context,
	target string,
	cachePath string,
	cached *model.Index,
) (*model.Index, CacheRefreshStatus, error) {
	if b == nil {
		return nil, "", fmt.Errorf("index builder is nil")
	}
	if cached == nil {
		return nil, "", fmt.Errorf("cached index is nil")
	}
	if strings.TrimSpace(cachePath) == "" {
		return nil, "", fmt.Errorf("cache path is empty")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	configChanged := !configHashesEqual(cached.ConfigHashes, b.configHashes)
	var report FreshnessReport
	if !configChanged {
		var err error
		report, err = b.CheckFreshness(ctx, target, cached)
		if err != nil {
			return nil, "", err
		}
		if report.IsFresh() {
			return cached, CacheUnchanged, nil
		}
	}

	base := cached
	status := CacheIncrementallyRefreshed
	if configChanged || report.RootMismatch {
		base = nil
		status = CacheFullyRebuilt
	}
	refreshed, _, err := b.BuildPathIncremental(ctx, target, base)
	if err != nil {
		return nil, "", err
	}
	if err := Save(cachePath, refreshed); err != nil {
		return nil, "", fmt.Errorf("save refreshed cache: %w", err)
	}
	return refreshed, status, nil
}

func configHashesEqual(cached, current map[string]string) bool {
	if len(cached) != len(current) {
		return false
	}
	for name, hash := range cached {
		if current[name] != hash {
			return false
		}
	}
	return true
}
