package main

import (
	"bytes"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/odvcencio/gts-suite/pkg/decompiler"
)

func newNormalizeCmd() *cobra.Command {
	var format string
	var output string
	var inPlace bool

	cmd := &cobra.Command{
		Use:   "normalize <file>",
		Short: "Normalize decompiler output for structural analysis",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := args[0]
			src, err := os.ReadFile(path)
			if err != nil {
				return fmt.Errorf("reading %s: %w", path, err)
			}

			detected := detectFormat(src, format)
			original := len(src)

			var result []byte
			switch detected {
			case "ghidra":
				result = decompiler.NormalizeGhidra(src)
			case "ida":
				result = decompiler.NormalizeIDA(src)
			default:
				result = src
			}

			cleaned := original - len(result)
			fmt.Fprintf(os.Stderr, "format=%s bytes_in=%d bytes_out=%d bytes_cleaned=%d\n",
				detected, original, len(result), cleaned)

			if inPlace {
				return os.WriteFile(path, result, 0644)
			}
			if output != "" {
				return os.WriteFile(output, result, 0644)
			}
			_, err = os.Stdout.Write(result)
			return err
		},
	}

	cmd.Flags().StringVar(&format, "format", "auto", "decompiler format (auto|ghidra|ida)")
	cmd.Flags().StringVarP(&output, "output", "o", "", "output file (default stdout)")
	cmd.Flags().BoolVar(&inPlace, "in-place", false, "write back to source file")
	return cmd
}

func detectFormat(src []byte, hint string) string {
	switch hint {
	case "ghidra":
		return "ghidra"
	case "ida":
		return "ida"
	}
	// Auto-detect
	if bytes.Contains(src, []byte("WARNING: This file")) ||
		bytes.Contains(src, []byte("/* WARNING:")) ||
		bytes.Contains(src, []byte("// WARNING:")) ||
		bytes.Contains(src, []byte("undefined4")) ||
		bytes.Contains(src, []byte("undefined8")) {
		return "ghidra"
	}
	if bytes.Contains(src, []byte("__int64")) ||
		bytes.Contains(src, []byte("__int32")) ||
		bytes.Contains(src, []byte("_DWORD")) ||
		bytes.Contains(src, []byte("_QWORD")) ||
		bytes.Contains(src, []byte("_BYTE")) {
		return "ida"
	}
	return "passthrough"
}
