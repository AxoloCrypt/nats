package ble

import "testing"

type fakeWriter struct {
	name string
}

func (f fakeWriter) Name() string                 { return f.name }
func (f fakeWriter) Write(Report) ([]byte, error) { return nil, nil }

func TestRegisterWriter_GetWriterFindsRegisteredName(t *testing.T) {
	orig := writerRegistry["fake"]
	defer func() {
		if orig != nil {
			writerRegistry["fake"] = orig
		} else {
			delete(writerRegistry, "fake")
		}
	}()

	RegisterWriter(fakeWriter{name: "fake"})

	w, ok := GetWriter("fake")
	if !ok {
		t.Fatal("expected GetWriter to find registered writer")
	}
	if w.Name() != "fake" {
		t.Fatalf("expected writer named 'fake', got %q", w.Name())
	}
}

func TestGetWriter_UnknownNameNotFound(t *testing.T) {
	_, ok := GetWriter("does-not-exist")
	if ok {
		t.Fatal("expected GetWriter to report not-found for an unregistered name")
	}
}

func TestWriterNames_SortedAndReflectsRegistry(t *testing.T) {
	orig := make(map[string]Writer, len(writerRegistry))
	for k, v := range writerRegistry {
		orig[k] = v
	}
	defer func() {
		writerRegistry = orig
	}()

	writerRegistry = map[string]Writer{}
	RegisterWriter(fakeWriter{name: "zeta"})
	RegisterWriter(fakeWriter{name: "alpha"})

	names := WriterNames()
	if len(names) != 2 || names[0] != "alpha" || names[1] != "zeta" {
		t.Fatalf("expected sorted [\"alpha\", \"zeta\"], got %v", names)
	}
}
