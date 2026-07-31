package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"m31labs.dev/canopy/pkg/changeintel"
)

// newReviewCmd is a thin adapter over pkg/changeintel.Service: it parses
// flags into a changeintel.Request, calls Analyze, and prints the resulting
// Receipt. All change-intelligence logic (structural deltas, complexity,
// API surface, boundaries, capabilities, risk) lives in pkg/changeintel.
func newReviewCmd() *cobra.Command {
	var (
		base          string
		head          string
		jsonOutput    bool
		includeTests  bool
		includeRisk   bool
		policyVersion string
	)

	cmd := &cobra.Command{
		Use:   "review [path]",
		Short: "Change receipt for changed files vs a base ref",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if base == "" {
				return fmt.Errorf("--base is required")
			}

			target := "."
			if len(args) == 1 {
				target = args[0]
			}

			svc := changeintel.NewService()
			receipt, err := svc.Analyze(cmd.Context(), changeintel.Request{
				Root:          target,
				Base:          changeintel.SnapshotRef{Ref: base},
				Head:          changeintel.SnapshotRef{Ref: head},
				IncludeTests:  includeTests,
				IncludeRisk:   includeRisk,
				PolicyVersion: policyVersion,
			})
			if err != nil {
				return err
			}

			if jsonOutput {
				return emitJSON(receipt)
			}
			printReviewReceipt(receipt)
			return nil
		},
	}

	cmd.Flags().StringVar(&base, "base", "", "git ref to diff against (required)")
	cmd.Flags().StringVar(&head, "head", "", "git ref for head (default: working tree)")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "emit the full change receipt as JSON")
	cmd.Flags().BoolVar(&includeTests, "include-tests", false, "compute test impact for changed entities")
	cmd.Flags().BoolVar(&includeRisk, "include-risk", false, "compute the routing risk score")
	cmd.Flags().StringVar(&policyVersion, "policy-version", "", "policy version recorded on the request for audit/reproducibility")
	return cmd
}

func printReviewReceipt(r changeintel.Receipt) {
	headLabel := r.Head.Ref
	if headLabel == "" {
		headLabel = "working tree"
	}

	fmt.Printf("review: base=%s head=%s changed_files=%d blast_radius=%d risk=%.2f\n",
		r.Base.Ref, headLabel, r.Impact.ChangedFiles, r.Impact.BlastRadius, r.Risk.Score)
	if !r.Impact.BaseAvailable {
		fmt.Println("warning: base ref unavailable — changed files reported as added")
	}

	if len(r.Entities) > 0 {
		fmt.Println("\nentities:")
		for _, e := range r.Entities {
			fmt.Printf("  %-9s %s %s cyc=%+d cog=%+d\n", e.Status, e.File, e.Name, e.Delta.Cyclomatic, e.Delta.Cognitive)
		}
	}

	changedAPI := len(r.APISurface.Added) + len(r.APISurface.Removed) + len(r.APISurface.Changed)
	if changedAPI > 0 {
		fmt.Printf("\npublic API changes: %d\n", changedAPI)
		for _, c := range r.APISurface.Added {
			fmt.Printf("  + %s %s\n", c.File, c.Name)
		}
		for _, c := range r.APISurface.Removed {
			fmt.Printf("  - %s %s\n", c.File, c.Name)
		}
		for _, c := range r.APISurface.Changed {
			fmt.Printf("  ~ %s %s (%s)\n", c.File, c.Name, c.Change)
		}
	}

	if len(r.Boundaries.Introduced) > 0 {
		fmt.Printf("\nboundary violations introduced: %d\n", len(r.Boundaries.Introduced))
		for _, v := range r.Boundaries.Introduced {
			fmt.Printf("  %s\n", v.Message)
		}
	}
	if len(r.Boundaries.Resolved) > 0 {
		fmt.Printf("\nboundary violations resolved: %d\n", len(r.Boundaries.Resolved))
	}

	if len(r.Capabilities.Introduced) > 0 {
		fmt.Printf("\ncapabilities introduced: %d\n", len(r.Capabilities.Introduced))
		for _, c := range r.Capabilities.Introduced {
			fmt.Printf("  %s (%s, %s)\n", c.Name, c.Category, c.Confidence)
		}
	}
	if len(r.Capabilities.Resolved) > 0 {
		fmt.Printf("\ncapabilities resolved: %d\n", len(r.Capabilities.Resolved))
	}
}
