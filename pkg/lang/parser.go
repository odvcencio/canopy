// Package lang defines the Parser interface for language-specific source file parsing.
package lang

import "github.com/odvcencio/canopy/pkg/model"

// Parser converts source files into structural summaries.
type Parser interface {
	// Language returns the name of the language this parser handles.
	Language() string
	// Parse analyzes a source file and returns its structural summary.
	Parse(path string, src []byte) (model.FileSummary, error)
}
