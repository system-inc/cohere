package flagged

// unix is a build tag, not a suffix go/build reads, and with no //go:build line the file is no variant of anything.
type Cache struct { // want "`Cache` is the one type .* Name it `cache.go`.$"
	entries map[string]string
}

func (cache *Cache) Get(key string) string {
	return cache.entries[key]
}
