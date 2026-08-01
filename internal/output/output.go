package output

import (
	"encoding/json"
	"io"
)

const APIVersion = "rungrid/output/v1"

type Diagnostic struct {
	Code     string `json:"code" yaml:"code"`
	Severity string `json:"severity" yaml:"severity"`
	Summary  string `json:"summary" yaml:"summary"`
	Path     string `json:"path,omitempty" yaml:"path,omitempty"`
	Detail   string `json:"detail,omitempty" yaml:"detail,omitempty"`
}

type Envelope struct {
	APIVersion  string       `json:"api_version"`
	Kind        string       `json:"kind"`
	ProjectID   string       `json:"project_id,omitempty"`
	Data        any          `json:"data"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

func WriteJSON(w io.Writer, kind, projectID string, data any, diagnostics []Diagnostic) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(Envelope{
		APIVersion:  APIVersion,
		Kind:        kind,
		ProjectID:   projectID,
		Data:        data,
		Diagnostics: diagnostics,
	})
}
