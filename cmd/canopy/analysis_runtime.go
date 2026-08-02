package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
)

const defaultAnalysisTimeout = 4*time.Minute + 45*time.Second

func startAnalysisPhase(name string) func() {
	done := make(chan struct{})
	started := time.Now()
	go func() {
		timer := time.NewTimer(2 * time.Second)
		defer timer.Stop()
		select {
		case <-done:
			return
		case <-timer.C:
			fmt.Fprintf(os.Stderr, "analysis: %s still running (%s)\n", name, time.Since(started).Truncate(time.Second))
		}

		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				fmt.Fprintf(os.Stderr, "analysis: %s still running (%s)\n", name, time.Since(started).Truncate(time.Second))
			}
		}
	}()
	return func() {
		close(done)
	}
}

func boundedAnalysisRun(timeout *time.Duration, run func(*cobra.Command, []string) error) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		limit := defaultAnalysisTimeout
		if timeout != nil && *timeout > 0 && *timeout < limit {
			limit = *timeout
		}
		parent := cmd.Context()
		if parent == nil {
			parent = context.Background()
		}
		ctx, cancel := context.WithTimeout(parent, limit)
		defer cancel()
		cmd.SetContext(ctx)

		result := make(chan error, 1)
		go func() {
			result <- run(cmd, args)
		}()
		select {
		case err := <-result:
			return err
		case <-ctx.Done():
			return exitCodeError{
				code: 2,
				err:  fmt.Errorf("analysis exceeded the hard %s runtime limit", limit),
			}
		}
	}
}
