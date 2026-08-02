package main

import (
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

func TestBoundedAnalysisRunReturnsCompletedResult(t *testing.T) {
	timeout := time.Second
	run := boundedAnalysisRun(&timeout, func(*cobra.Command, []string) error {
		return nil
	})
	if err := run(&cobra.Command{}, nil); err != nil {
		t.Fatalf("boundedAnalysisRun() error = %v", err)
	}
}

func TestBoundedAnalysisRunEnforcesShorterLimit(t *testing.T) {
	timeout := 10 * time.Millisecond
	run := boundedAnalysisRun(&timeout, func(*cobra.Command, []string) error {
		time.Sleep(time.Second)
		return nil
	})
	err := run(&cobra.Command{}, nil)
	if err == nil || !strings.Contains(err.Error(), "hard 10ms runtime limit") {
		t.Fatalf("boundedAnalysisRun() error = %v, want hard timeout", err)
	}
	if withCode, ok := err.(interface{ ExitCode() int }); !ok || withCode.ExitCode() != 2 {
		t.Fatalf("boundedAnalysisRun() error = %T %v, want exit code 2", err, err)
	}
}
