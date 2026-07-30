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
	}
}
