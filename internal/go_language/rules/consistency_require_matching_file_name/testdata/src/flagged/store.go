package flagged

// Registry is the one type with methods this file serves, and the file is named for something else.
type Registry struct { // want "`Registry` is the one type with methods served in `store.go`, .* `registry.go`.$"
	entries map[string]int
}

// Options has no methods, so it never counts beside Registry.
type Options struct {
	limit int
}

func (registry *Registry) Add(name string) {
	registry.entries[name]++
}
