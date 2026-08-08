package ble

import "sort"

// scanner holds the single registered BLEScanner. Mirrors
// core/engine/registry.go's self-registration pattern so a future Android
// companion-app bridge (deferred) can register a second BLEScanner
// implementation later without core/ble changing at all.
var scanner BLEScanner

// writerRegistry holds every self-registered Writer, keyed by format name —
// mirrors core/engine/registry.go's writerRegistry exactly.
var writerRegistry = map[string]Writer{}

func RegisterScanner(s BLEScanner) {
	scanner = s
}

func GetScanner() (BLEScanner, bool) {
	return scanner, scanner != nil
}

func RegisterWriter(w Writer) {
	writerRegistry[w.Name()] = w
}

func GetWriter(name string) (Writer, bool) {
	w, ok := writerRegistry[name]
	return w, ok
}

// WriterNames returns the names of all registered writers, sorted, so
// cmd/cli can report the currently-available format names on an invalid
// --format without hardcoding a list that can drift out of sync with the
// registry.
func WriterNames() []string {
	names := make([]string, 0, len(writerRegistry))
	for name := range writerRegistry {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
