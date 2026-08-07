// Package json renders a BLE Report as machine-readable JSON (spine AD-11),
// mirroring report/json's structure. It consumes only core/ble's final
// Report struct, never core/ble's live scan state.
package json

import (
	"encoding/json"

	"nats/core/ble"
)

func init() {
	ble.RegisterWriter(Writer{})
}

// Writer renders a BLE Report as indented JSON.
type Writer struct{}

// Name implements ble.Writer, registering this writer under the "json"
// format name.
func (Writer) Name() string {
	return "json"
}

func (Writer) Write(r ble.Report) ([]byte, error) {
	// A nil slice marshals to JSON "null", not "[]" — for a Report with zero
	// devices (or, in the ordinary case, zero diagnostics), an explicit empty
	// array is the well-formed output a scripting consumer's JSON parser
	// expects, so both are normalized here before marshaling (only affects
	// this local copy of r, taken by value).
	if r.Devices == nil {
		r.Devices = []ble.BLEDeviceProfile{}
	}
	if r.Diagnostics == nil {
		r.Diagnostics = []ble.Diagnostic{}
	}
	out, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}
