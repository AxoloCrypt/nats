package engine

import (
	"context"
	"testing"
)

type fakeEnricher struct {
	name              string
	requiresPrivilege bool
}

func (f *fakeEnricher) Name() string            { return f.name }
func (f *fakeEnricher) RequiresPrivilege() bool { return f.requiresPrivilege }
func (f *fakeEnricher) Enrich(ctx context.Context, device Device) (Device, error) {
	return device, nil
}

func TestRegisterEnricher_GetEnricherFindsRegisteredName(t *testing.T) {
	orig := enricherRegistry["fake"]
	defer func() {
		if orig != nil {
			enricherRegistry["fake"] = orig
		} else {
			delete(enricherRegistry, "fake")
		}
	}()

	RegisterEnricher(&fakeEnricher{name: "fake"})

	e, ok := GetEnricher("fake")
	if !ok {
		t.Fatal("expected GetEnricher to find registered enricher")
	}
	if e.Name() != "fake" {
		t.Fatalf("expected enricher named 'fake', got %q", e.Name())
	}
}

func TestGetEnricher_UnknownNameNotFound(t *testing.T) {
	_, ok := GetEnricher("does-not-exist")
	if ok {
		t.Fatal("expected GetEnricher to report not-found for an unregistered name")
	}
}
