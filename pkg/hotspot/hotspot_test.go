package hotspot

import (
	"math"
	"testing"
)

func TestParseGitLogEmpty(t *testing.T) {
	result := ParseGitLog("")
	if len(result) != 0 {
		t.Fatalf("expected empty, got %d entries", len(result))
	}
}

func TestParseGitLog(t *testing.T) {
	log := `Alice

10	2	main.go
5	1	util.go

Bob

3	0	main.go
1	1	config.go

Alice

2	0	main.go
`
	result := ParseGitLog(log)

	main := result["main.go"]
	if main.Commits != 3 {
		t.Errorf("main.go commits: got %d, want 3", main.Commits)
	}
	if main.Authors != 2 {
		t.Errorf("main.go authors: got %d, want 2", main.Authors)
	}

	util := result["util.go"]
	if util.Commits != 1 {
		t.Errorf("util.go commits: got %d, want 1", util.Commits)
	}
	if util.Authors != 1 {
		t.Errorf("util.go authors: got %d, want 1", util.Authors)
	}

	config := result["config.go"]
	if config.Commits != 1 {
		t.Errorf("config.go commits: got %d, want 1", config.Commits)
	}
}

func TestNormalizeSince(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"90d", "90 days ago"},
		{"6m", "6 months ago"},
		{"1y", "1 years ago"},
		{"2w", "2 weeks ago"},
		{"", "6 months ago"},
		{"2024-01-01", "2024-01-01"},
		{"3 months ago", "3 months ago"},
	}
	for _, tc := range tests {
		got := normalizeSince(tc.input)
		if got != tc.want {
			t.Errorf("normalizeSince(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestPercentileRank(t *testing.T) {
	values := []float64{10, 20, 30, 40, 50}
	ranks := percentileRank(values)

	if len(ranks) != 5 {
		t.Fatalf("expected 5 ranks, got %d", len(ranks))
	}

	// First (smallest) should be 0.0, last (largest) should be 1.0.
	if ranks[0] != 0.0 {
		t.Errorf("rank[0] = %f, want 0.0", ranks[0])
	}
	if ranks[4] != 1.0 {
		t.Errorf("rank[4] = %f, want 1.0", ranks[4])
	}

	// Middle should be 0.5.
	if ranks[2] != 0.5 {
		t.Errorf("rank[2] = %f, want 0.5", ranks[2])
	}
}

func TestPercentileRankSingleValue(t *testing.T) {
	ranks := percentileRank([]float64{42})
	if len(ranks) != 1 || ranks[0] != 1.0 {
		t.Errorf("single value rank: got %v, want [1.0]", ranks)
	}
}

func TestGeometricMean(t *testing.T) {
	// Equal values.
	score := geometricMean(0.5, 0.5, 0.5)
	if math.Abs(score-0.5) > 0.001 {
		t.Errorf("geometricMean(0.5,0.5,0.5) = %f, want ~0.5", score)
	}

	// All ones.
	score = geometricMean(1.0, 1.0, 1.0)
	if math.Abs(score-1.0) > 0.001 {
		t.Errorf("geometricMean(1,1,1) = %f, want ~1.0", score)
	}

	// One zero dimension uses epsilon.
	score = geometricMean(0.0, 1.0, 1.0)
	if score <= 0 {
		t.Errorf("geometricMean(0,1,1) should be > 0 (epsilon prevents zero)")
	}
	if score >= 1.0 {
		t.Errorf("geometricMean(0,1,1) = %f, should be < 1.0", score)
	}
}

func TestAnalyzeNilIndex(t *testing.T) {
	report, err := Analyze(nil, Options{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(report.Functions) != 0 {
		t.Errorf("expected 0 functions, got %d", len(report.Functions))
	}
}

func TestNormalizeChurnMapSubdirScanRoot(t *testing.T) {
	raw := map[string]FileChurn{
		"testdata/mini-estate/ACCT0001.cbl": {Commits: 4, Authors: 1},
		"testdata/mini-estate/ACCT0100.cbl": {Commits: 2, Authors: 1},
	}

	got := normalizeChurnMap(raw, "/repo/testdata/mini-estate", "/repo")

	if _, ok := got["testdata/mini-estate/ACCT0001.cbl"]; ok {
		t.Fatalf("normalizeChurnMap(...) kept repo-relative path, want scan-root-relative keys: %#v", got)
	}
	if churn := got["ACCT0001.cbl"]; churn.Commits != 4 || churn.Authors != 1 {
		t.Fatalf("normalizeChurnMap(...) ACCT0001.cbl = %#v, want commits=4 authors=1", churn)
	}
	if churn := got["ACCT0100.cbl"]; churn.Commits != 2 || churn.Authors != 1 {
		t.Fatalf("normalizeChurnMap(...) ACCT0100.cbl = %#v, want commits=2 authors=1", churn)
	}
}

func TestRankChurn(t *testing.T) {
	hotspots := []FunctionHotspot{
		{Commits: 1, Authors: 1},
		{Commits: 5, Authors: 2},
		{Commits: 10, Authors: 3},
	}
	rankChurn(hotspots)

	// First should have lowest churn rank, last should have highest.
	if hotspots[0].Churn >= hotspots[1].Churn {
		t.Errorf("churn rank: %f should be < %f", hotspots[0].Churn, hotspots[1].Churn)
	}
	if hotspots[1].Churn >= hotspots[2].Churn {
		t.Errorf("churn rank: %f should be < %f", hotspots[1].Churn, hotspots[2].Churn)
	}
}

func TestRankComplexity(t *testing.T) {
	hotspots := []FunctionHotspot{
		{Cyclomatic: 1},
		{Cyclomatic: 5},
		{Cyclomatic: 10},
	}
	rankComplexity(hotspots)

	if hotspots[0].Complexity >= hotspots[2].Complexity {
		t.Errorf("complexity rank: %f should be < %f", hotspots[0].Complexity, hotspots[2].Complexity)
	}
}

func TestRankCentrality(t *testing.T) {
	hotspots := []FunctionHotspot{
		{FanIn: 0},
		{FanIn: 3},
		{FanIn: 10},
	}
	rankCentrality(hotspots)

	if hotspots[0].Centrality >= hotspots[2].Centrality {
		t.Errorf("centrality rank: %f should be < %f", hotspots[0].Centrality, hotspots[2].Centrality)
	}
}
