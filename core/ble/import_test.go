package ble

import (
	"os/exec"
	"strings"
	"testing"
)

func TestBLEDoesNotImportCmdOrEngine(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", "./").Output()
	if err != nil {
		t.Fatalf("go list failed: %v", err)
	}

	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "nats/cmd/") {
			t.Fatalf("core/ble must not import cmd/ packages, but depends on: %s", line)
		}
		if line == "nats/core/engine" {
			t.Fatalf("core/ble must not import core/engine, but depends on: %s", line)
		}
		// The third root NL-AD-1 forbids. core/wifimonitor does not exist yet
		// (Epic 5), so this check is inert today — but it is checked here
		// precisely so the boundary is already guarded when that package
		// lands, rather than relying on someone remembering to add it then.
		if line == "nats/core/wifimonitor" || strings.HasPrefix(line, "nats/core/wifimonitor/") {
			t.Fatalf("core/ble must not import core/wifimonitor, but depends on: %s", line)
		}
	}
}
