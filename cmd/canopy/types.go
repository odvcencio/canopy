package main

type grepMatch struct {
	File      string `json:"file"`
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Signature string `json:"signature,omitempty"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
}

type referenceMatch struct {
	File        string `json:"file"`
	Kind        string `json:"kind"`
	Name        string `json:"name"`
	StartLine   int    `json:"start_line"`
	EndLine     int    `json:"end_line"`
	StartColumn int    `json:"start_column"`
	EndColumn   int    `json:"end_column"`
	Generated   string `json:"generated,omitempty"`
}

type queryCaptureMatch struct {
	File        string `json:"file"`
	Language    string `json:"language"`
	Pattern     int    `json:"pattern"`
	Capture     string `json:"capture"`
	NodeType    string `json:"node_type"`
	Text        string `json:"text"`
	StartLine   int    `json:"start_line"`
	EndLine     int    `json:"end_line"`
	StartColumn int    `json:"start_column"`
	EndColumn   int    `json:"end_column"`
}

type queryLanguageError struct {
	Language string `json:"language"`
	Error    string `json:"error"`
}

type structuralGrepMatch struct {
	File      string            `json:"file"`
	StartLine int               `json:"start_line"`
	EndLine   int               `json:"end_line"`
	Text      string            `json:"text"`
	Captures  map[string]string `json:"captures,omitempty"`
}

type deadMatch struct {
	File      string `json:"file"`
	Package   string `json:"package"`
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Signature string `json:"signature,omitempty"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
	Incoming  int    `json:"incoming"`
	Outgoing  int    `json:"outgoing"`
}
