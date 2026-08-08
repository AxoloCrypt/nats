// Package json renders a Report as machine-readable JSON, the format aimed
// at scripting/integration use cases. It consumes only the engine's final
// Report struct, never core/engine's live state.
package json

import (
	"encoding/json"

	"nats/core/engine"
)

func init() {
	engine.RegisterWriter(Writer{})
}

// Writer renders a Report as indented JSON.
type Writer struct{}

// Name implements engine.Writer, registering this writer under the "json"
// format name.
func (Writer) Name() string {
	return "json"
}

func (Writer) Write(r engine.Report) ([]byte, error) {
	out, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}
