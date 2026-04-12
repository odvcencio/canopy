// Package sarif provides a minimal SARIF 2.1.0 encoder for canopy
// analysis output. It produces valid SARIF JSON with no external dependencies.
package sarif

import (
	"encoding/json"
	"io"
)

// Log is the top-level SARIF 2.1.0 object.
type Log struct {
	Version string `json:"version"`
	Schema  string `json:"$schema"`
	Runs    []Run  `json:"runs"`
}

// Run groups results under a single tool invocation.
type Run struct {
	Tool    Tool     `json:"tool"`
	Results []Result `json:"results"`
}

// Tool identifies the analysis tool that produced the results.
type Tool struct {
	Driver Driver `json:"driver"`
}

// Driver describes the primary analysis component.
type Driver struct {
	Name           string                `json:"name"`
	Version        string                `json:"version,omitempty"`
	InformationURI string                `json:"informationUri,omitempty"`
	Rules          []ReportingDescriptor `json:"rules,omitempty"`
}

// ReportingDescriptor defines a rule referenced by results.
type ReportingDescriptor struct {
	ID               string  `json:"id"`
	ShortDescription Message `json:"shortDescription,omitempty"`
}

// Message holds human-readable text.
type Message struct {
	Text string `json:"text"`
}

// Result represents a single finding (violation).
type Result struct {
	RuleID    string     `json:"ruleId"`
	Level     string     `json:"level"`
	Message   Message    `json:"message"`
	Locations []Location `json:"locations,omitempty"`
}

// Location points to a specific place in an artifact.
type Location struct {
	PhysicalLocation PhysicalLocation `json:"physicalLocation"`
}

// PhysicalLocation identifies a file and optional region.
type PhysicalLocation struct {
	ArtifactLocation ArtifactLocation `json:"artifactLocation"`
	Region           *Region          `json:"region,omitempty"`
}

// ArtifactLocation is a URI-based reference to a file.
type ArtifactLocation struct {
	URI string `json:"uri"`
}

// Region identifies a span of lines in a file.
type Region struct {
	StartLine int `json:"startLine,omitempty"`
	EndLine   int `json:"endLine,omitempty"`
}

const (
	schemaURI = "https://docs.oasis-open.org/sarif/sarif/v2.1.0/errata01/os/schemas/sarif-schema-2.1.0.json"
	sarifVer  = "2.1.0"
)

// NewLog creates a SARIF log with a single run for canopy.
func NewLog() *Log {
	return &Log{
		Version: sarifVer,
		Schema:  schemaURI,
		Runs: []Run{
			{
				Tool: Tool{
					Driver: Driver{
						Name:           "canopy",
						InformationURI: "https://github.com/odvcencio/canopy",
					},
				},
			},
		},
	}
}

// AddRule adds a reporting descriptor (rule definition) to the first run.
func (l *Log) AddRule(id, description string) {
	l.Runs[0].Tool.Driver.Rules = append(l.Runs[0].Tool.Driver.Rules, ReportingDescriptor{
		ID:               id,
		ShortDescription: Message{Text: description},
	})
}

// AddResult adds a finding to the first run.
// If file is empty, no location is attached. If startLine or endLine are <= 0,
// the region is omitted.
func (l *Log) AddResult(ruleID, level, message, file string, startLine, endLine int) {
	r := Result{
		RuleID:  ruleID,
		Level:   level,
		Message: Message{Text: message},
	}
	if file != "" {
		loc := Location{
			PhysicalLocation: PhysicalLocation{
				ArtifactLocation: ArtifactLocation{URI: file},
			},
		}
		if startLine > 0 || endLine > 0 {
			loc.PhysicalLocation.Region = &Region{
				StartLine: startLine,
				EndLine:   endLine,
			}
		}
		r.Locations = []Location{loc}
	}
	l.Runs[0].Results = append(l.Runs[0].Results, r)
}

// Encode writes the SARIF JSON to w with indentation.
func (l *Log) Encode(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(l)
}

// MapSeverity converts canopy severity strings to SARIF level values.
// Unknown severities default to "warning".
func MapSeverity(severity string) string {
	switch severity {
	case "error":
		return "error"
	case "warn", "warning":
		return "warning"
	case "note", "info":
		return "note"
	default:
		return "warning"
	}
}
