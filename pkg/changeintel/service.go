package changeintel

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"m31labs.dev/canopy/pkg/index"
	"m31labs.dev/canopy/pkg/model"
	"m31labs.dev/canopy/pkg/structdiff"
	"m31labs.dev/canopy/pkg/testmap"
	"m31labs.dev/canopy/pkg/xref"
)

// Clock returns the current time. Service uses it to stamp
// Receipt.CreatedAt. Tests can pin it via WithClock; every other Receipt
// field is a pure function of Request and repository content.
type Clock func() time.Time

type service struct {
	now Clock
}

// Option configures a Service built by NewService.
type Option func(*service)

// WithClock overrides the clock used to stamp Receipt.CreatedAt.
func WithClock(clock Clock) Option {
	return func(s *service) {
		if clock != nil {
			s.now = clock
		}
	}
}

// NewService returns the production Service implementation.
func NewService(opts ...Option) Service {
	s := &service{now: func() time.Time { return time.Now().UTC() }}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Analyze implements Service. It always operates in "changed_only" index
// scope (see ImpactSummary.IndexScope): base content is read via `git show`
// and head content via the working tree or `git show`, so no temporary
// worktree is ever materialized. This is the mode spec section 11.2
// describes; a "full" repository scope (reusing Canopy's existing
// disk-walking Builder for one side) is not implemented by this package and
// is left to a future PR — see the top-level report for this decision.
func (s *service) Analyze(ctx context.Context, req Request) (Receipt, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	root := strings.TrimSpace(req.Root)
	if root == "" {
		root = "."
	}

	baseAvailable := gitVerifyRef(ctx, root, req.Base.Ref)

	var (
		files                    []FileChange
		baseContent, headContent map[string][]byte
		err                      error
	)
	switch {
	case len(req.ChangedPaths) > 0:
		files, baseContent, headContent = changesFromExplicitPaths(ctx, root, req, baseAvailable)
	case baseAvailable:
		files, baseContent, headContent, err = changesFromGitDiff(ctx, root, req)
		if err != nil {
			return Receipt{}, err
		}
	default:
		baseContent, headContent = map[string][]byte{}, map[string][]byte{}
	}

	builder := index.NewBuilder()

	// Both indexes get Root == the real repository root (not a synthetic
	// "base:<ref>" label): deps.Build and xref.Build resolve the Go module
	// path via Index.Root to classify imports as internal, and base/head
	// are two revisions of the same module, so they share one root.
	baseIdx, err := builder.BuildSources(ctx, root, toSourceFiles(baseContent))
	if err != nil {
		return Receipt{}, fmt.Errorf("building base index: %w", err)
	}
	headIdx, err := builder.BuildSources(ctx, root, toSourceFiles(headContent))
	if err != nil {
		return Receipt{}, fmt.Errorf("building head index: %w", err)
	}

	files = enrichFileChanges(files, baseIdx, headIdx, baseContent, headContent)

	fileLang := make(map[string]string, len(files))
	for _, fc := range files {
		if fc.Language != "" {
			fileLang[fc.Path] = fc.Language
		}
	}

	entities, complexityDelta, apiDelta := analyzeEntities(files, baseIdx, headIdx, baseContent, headContent, fileLang)

	renameMap := make(map[string]string)
	for _, fc := range files {
		if fc.Status == FileRenamed {
			renameMap[fc.OldPath] = fc.Path
		}
	}
	diffReport := structdiff.Compare(rebaseIndexPaths(baseIdx, renameMap), headIdx)
	importChanges := toImportChanges(diffReport.ImportChanges)

	boundaryDelta := computeBoundaryDelta(ctx, root, req.Head, baseIdx, headIdx)
	capabilityDelta := computeCapabilityDelta(baseIdx, headIdx)

	blastRadius, affectedFiles := computeBlastRadius(headIdx, entities)
	impactSummary := buildImpactSummary(files, entities, apiDelta, blastRadius, affectedFiles, baseAvailable)

	var testImpact TestImpact
	if req.IncludeTests {
		testImpact = computeTestImpact(headIdx, entities)
	}

	var riskDelta RiskDelta
	if req.IncludeRisk {
		riskDelta = computeRisk(entities, files, complexityDelta, apiDelta, boundaryDelta, capabilityDelta, impactSummary, testImpact, req.IncludeTests)
	}

	receipt := Receipt{
		SchemaVersion: SchemaVersion,
		Root:          root,
		Base:          req.Base,
		Head:          req.Head,
		Files:         files,
		Entities:      entities,
		Imports:       importChanges,
		Complexity:    complexityDelta,
		APISurface:    apiDelta,
		Boundaries:    boundaryDelta,
		Capabilities:  capabilityDelta,
		Impact:        impactSummary,
		Tests:         testImpact,
		Risk:          riskDelta,
		CreatedAt:     s.now(),
	}

	digest := computeDigest(receipt)
	receipt.Digest = digest
	receipt.ID = "sha256:" + digest

	return receipt, nil
}

func toSourceFiles(content map[string][]byte) []index.SourceFile {
	paths := make([]string, 0, len(content))
	for p := range content {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	files := make([]index.SourceFile, 0, len(paths))
	for _, p := range paths {
		files = append(files, index.SourceFile{Path: p, Content: content[p]})
	}
	return files
}

// changesFromGitDiff discovers changed paths via `git diff --name-status -M`
// and fetches base/head content for each one.
func changesFromGitDiff(ctx context.Context, root string, req Request) ([]FileChange, map[string][]byte, map[string][]byte, error) {
	raws, err := gitDiffNameStatus(ctx, root, req.Base.Ref, req.Head.Ref)
	if err != nil {
		return nil, nil, nil, err
	}

	baseContent := map[string][]byte{}
	headContent := map[string][]byte{}
	files := make([]FileChange, 0, len(raws))

	for _, rc := range raws {
		status := classifyStatus(rc.code)
		fc := FileChange{Path: rc.path, Status: status}
		baseFilePath := rc.path
		if status == FileRenamed {
			baseFilePath = rc.oldPath
			fc.OldPath = rc.oldPath
		}

		if status != FileAdded {
			if bc, ok, err := snapshotContent(ctx, root, req.Base, baseFilePath); err == nil && ok {
				baseContent[baseFilePath] = bc
			}
		}
		if status != FileRemoved {
			if hc, ok, err := snapshotContent(ctx, root, req.Head, rc.path); err == nil && ok {
				headContent[rc.path] = hc
			}
		}
		files = append(files, fc)
	}
	return files, baseContent, headContent, nil
}

// changesFromExplicitPaths classifies a caller-supplied path set by
// existence on each side rather than via `git diff`. When base is
// unavailable every path is reported as added, since there is no base
// content to compare against.
func changesFromExplicitPaths(ctx context.Context, root string, req Request, baseAvailable bool) ([]FileChange, map[string][]byte, map[string][]byte) {
	baseContent := map[string][]byte{}
	headContent := map[string][]byte{}

	paths := make([]string, 0, len(req.ChangedPaths))
	seen := map[string]bool{}
	for _, p := range req.ChangedPaths {
		clean := strings.TrimSpace(filepath.ToSlash(p))
		if clean == "" || seen[clean] {
			continue
		}
		seen[clean] = true
		paths = append(paths, clean)
	}
	sort.Strings(paths)

	files := make([]FileChange, 0, len(paths))
	for _, p := range paths {
		fc := FileChange{Path: p}

		headBytes, headExists, _ := snapshotContent(ctx, root, req.Head, p)
		if headExists {
			headContent[p] = headBytes
		}

		if !baseAvailable {
			fc.Status = FileAdded
			files = append(files, fc)
			continue
		}

		baseBytes, baseExists, _ := snapshotContent(ctx, root, req.Base, p)
		if baseExists {
			baseContent[p] = baseBytes
		}

		switch {
		case baseExists && headExists:
			fc.Status = FileModified
		case !baseExists && headExists:
			fc.Status = FileAdded
		case baseExists && !headExists:
			fc.Status = FileRemoved
		default:
			// Caller claimed this path changed but it exists on neither
			// side (for example a rename target/source we weren't told
			// about); report it as modified rather than dropping it
			// silently.
			fc.Status = FileModified
		}
		files = append(files, fc)
	}
	return files, baseContent, headContent
}

func classifyStatus(code string) FileStatus {
	switch {
	case strings.HasPrefix(code, "R"):
		return FileRenamed
	case code == "A":
		return FileAdded
	case code == "D":
		return FileRemoved
	default:
		return FileModified
	}
}

func errorSet(idx *model.Index) map[string]bool {
	out := map[string]bool{}
	if idx == nil {
		return out
	}
	for _, e := range idx.Errors {
		out[e.Path] = true
	}
	return out
}

// enrichFileChanges fills in Language/Generated/*Indexed/Uncertain from the
// already-built base/head indexes and sorts the result by path.
//
// Uncertain marks a file whose content changed but whose structural shape
// Canopy cannot describe: either a genuine parse failure (recorded in the
// index's Errors) or, far more commonly given gotreesitter's tolerant error
// recovery, a file with no registered parser at all for its language (a
// changed .json/.png/unsupported-language file, for example). In both
// cases entity, complexity, and API deltas touching that path are silently
// incomplete, so Analyze surfaces it instead of hiding it.
func enrichFileChanges(files []FileChange, baseIdx, headIdx *model.Index, baseContent, headContent map[string][]byte) []FileChange {
	baseFiles := indexFilesByPath(baseIdx)
	headFiles := indexFilesByPath(headIdx)
	baseErrs := errorSet(baseIdx)
	headErrs := errorSet(headIdx)

	for i := range files {
		fc := &files[i]
		baseFilePath := fc.Path
		if fc.Status == FileRenamed {
			baseFilePath = fc.OldPath
		}

		if hf, ok := headFiles[fc.Path]; ok {
			fc.HeadIndexed = true
			fc.Language = hf.Language
			if hf.Generated != nil {
				fc.Generated = true
			}
		}
		if bf, ok := baseFiles[baseFilePath]; ok {
			fc.BaseIndexed = true
			if fc.Language == "" {
				fc.Language = bf.Language
			}
			if bf.Generated != nil {
				fc.Generated = true
			}
		}

		if baseErrs[baseFilePath] || headErrs[fc.Path] {
			fc.Uncertain = true
		}
		if fc.Status != FileAdded && !fc.BaseIndexed {
			if _, hasContent := baseContent[baseFilePath]; hasContent {
				fc.Uncertain = true
			}
		}
		if fc.Status != FileRemoved && !fc.HeadIndexed {
			if _, hasContent := headContent[fc.Path]; hasContent {
				fc.Uncertain = true
			}
		}
	}

	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files
}

// rebaseIndexPaths returns a copy of idx with every file (and its symbols'
// and references' File fields) renamed per renameMap, so a renamed file's
// base-side entities compare against the same key as their head-side
// counterparts.
func rebaseIndexPaths(idx *model.Index, renameMap map[string]string) *model.Index {
	if idx == nil || len(renameMap) == 0 {
		return idx
	}
	out := *idx
	out.Files = make([]model.FileSummary, len(idx.Files))
	for i, f := range idx.Files {
		newPath, renamed := renameMap[f.Path]
		if !renamed {
			out.Files[i] = f
			continue
		}
		nf := f
		nf.Path = newPath
		if len(f.Symbols) > 0 {
			nf.Symbols = make([]model.Symbol, len(f.Symbols))
			for j, sym := range f.Symbols {
				sym.File = newPath
				nf.Symbols[j] = sym
			}
		}
		if len(f.References) > 0 {
			nf.References = make([]model.Reference, len(f.References))
			for j, ref := range f.References {
				ref.File = newPath
				nf.References[j] = ref
			}
		}
		out.Files[i] = nf
	}
	return &out
}

// computeBlastRadius walks the reverse call graph of headIdx from every
// changed entity that still exists in head, using the same xref.Graph.Walk
// primitive pkg/impact and pkg/testmap already depend on. It is necessarily
// scoped to whatever headIdx contains — see the package doc for why that is
// "changed_only" in this implementation.
func computeBlastRadius(headIdx *model.Index, entities []EntityChange) (int, []string) {
	if headIdx == nil || len(entities) == 0 {
		return 0, nil
	}
	graph, err := xref.Build(headIdx)
	if err != nil {
		return 0, nil
	}

	rootSet := map[string]bool{}
	var rootIDs []string
	for _, e := range entities {
		if e.Status == EntityRemoved {
			continue
		}
		for _, def := range graph.Definitions {
			if def.File == e.File && def.Name == e.Name && def.Kind == e.Kind && !rootSet[def.ID] {
				rootSet[def.ID] = true
				rootIDs = append(rootIDs, def.ID)
			}
		}
	}
	if len(rootIDs) == 0 {
		return 0, nil
	}

	walk := graph.Walk(rootIDs, 5, true)
	fileSet := map[string]bool{}
	count := 0
	for _, node := range walk.Nodes {
		if rootSet[node.ID] {
			continue
		}
		count++
		fileSet[node.File] = true
	}
	files := make([]string, 0, len(fileSet))
	for f := range fileSet {
		files = append(files, f)
	}
	sort.Strings(files)
	return count, files
}

func buildImpactSummary(files []FileChange, entities []EntityChange, apiDelta APIDelta, blastRadius int, affectedFiles []string, baseAvailable bool) ImpactSummary {
	summary := ImpactSummary{
		ChangedFiles:  len(files),
		IndexScope:    "changed_only",
		BaseAvailable: baseAvailable,
		BlastRadius:   blastRadius,
		AffectedFiles: affectedFiles,
	}
	for _, e := range entities {
		switch e.Status {
		case EntityAdded:
			summary.AddedEntities++
			summary.ChangedEntities++
		case EntityRemoved:
			summary.RemovedEntities++
			summary.ChangedEntities++
		case EntityModified:
			summary.ModifiedEntities++
			summary.ChangedEntities++
		}
	}
	summary.ChangedPublicAPI = len(apiDelta.Added) + len(apiDelta.Removed) + len(apiDelta.Changed)

	for _, f := range files {
		if f.Uncertain {
			summary.ParseUncertainty = append(summary.ParseUncertainty, f.Path)
		}
		if f.Generated {
			summary.GeneratedInvolved = append(summary.GeneratedInvolved, f.Path)
		}
	}
	return summary
}

func computeTestImpact(headIdx *model.Index, entities []EntityChange) TestImpact {
	if headIdx == nil {
		return TestImpact{}
	}
	report, err := testmap.Map(headIdx, testmap.Options{})
	if err != nil {
		return TestImpact{}
	}

	changedNames := map[string]bool{}
	for _, e := range entities {
		if e.Status == EntityRemoved {
			continue
		}
		changedNames[e.File+"\x00"+e.Name] = true
	}

	var mapped, untested []string
	testedCount := 0
	for _, m := range report.Mappings {
		if !changedNames[m.File+"\x00"+m.Symbol] {
			continue
		}
		if len(m.Tests) == 0 {
			untested = append(untested, m.File+"::"+m.Symbol)
			continue
		}
		testedCount++
		for _, t := range m.Tests {
			mapped = append(mapped, t.File+"::"+t.Name)
		}
	}

	mapped = dedupSortedStrings(mapped)
	sort.Strings(untested)

	total := testedCount + len(untested)
	var coverage float64
	if total > 0 {
		coverage = float64(testedCount) / float64(total)
	}

	return TestImpact{
		MappedTests:             mapped,
		SelectedTests:           mapped,
		UntestedChangedEntities: untested,
		Coverage:                coverage,
	}
}

// toImportChanges adapts structdiff.FileImportChange (pkg/structdiff's
// existing import-diff type) to changeintel's own ImportChange so the
// receipt schema does not couple to structdiff's package layout.
func toImportChanges(items []structdiff.FileImportChange) []ImportChange {
	out := make([]ImportChange, 0, len(items))
	for _, item := range items {
		out = append(out, ImportChange{File: item.File, Added: item.Added, Removed: item.Removed})
	}
	return out
}

func dedupSortedStrings(items []string) []string {
	sort.Strings(items)
	out := items[:0]
	var prev string
	for i, v := range items {
		if i > 0 && v == prev {
			continue
		}
		out = append(out, v)
		prev = v
	}
	return out
}
