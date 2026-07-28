package engine

import "sort"

var registry = map[string]DiscoveryTechnique{}
var enricherRegistry = map[string]Enricher{}
var writerRegistry = map[string]Writer{}

func RegisterTechnique(t DiscoveryTechnique) {
	registry[t.Name()] = t
}

func GetTechnique(name string) (DiscoveryTechnique, bool) {
	t, ok := registry[name]
	return t, ok
}

func RegisterEnricher(e Enricher) {
	enricherRegistry[e.Name()] = e
}

func GetEnricher(name string) (Enricher, bool) {
	e, ok := enricherRegistry[name]
	return e, ok
}

func RegisterWriter(w Writer) {
	writerRegistry[w.Name()] = w
}

func GetWriter(name string) (Writer, bool) {
	w, ok := writerRegistry[name]
	return w, ok
}

// WriterNames returns the names of all registered writers, sorted, so
// callers can report the currently-available format names without
// hardcoding a list that can drift out of sync with the registry.
func WriterNames() []string {
	names := make([]string, 0, len(writerRegistry))
	for name := range writerRegistry {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
