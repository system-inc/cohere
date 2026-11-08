// Package differences holds the shapes where the port and the original could most plausibly part, and records none,
// since they part nowhere.
package differences

import "net/http"

type shadow struct{}

func (shadow) CanonicalHeaderKey(key string) string { return key }

type nested struct {
	inner struct {
		http.Header
	}
}

func generic[T ~map[string][]string](headers T, key string) {
	http.Header(headers).Set(http.CanonicalHeaderKey(key), "value") // found inside a generic function
}

func shapes(headers http.Header, key string) {
	// A local named http hides the package, so its CanonicalHeaderKey is a method.
	{
		http := shadow{}
		headers.Set(http.CanonicalHeaderKey(key), "value")
	}
	var deep nested
	deep.inner.Set(http.CanonicalHeaderKey(key), "value")                                    // found through an embedding in a field
	headers.Set(http.CanonicalHeaderKey(headers.Get(http.CanonicalHeaderKey(key))), "value") // found twice, nested
	headers.Set(http.CanonicalHeaderKey(
		// The key, explained.
		key+
			"-suffix",
	), "value") // found, the fix drops the comment and keeps the line break
	headers.Add(http.CanonicalHeaderKey(key), http.CanonicalHeaderKey(key)) // found once, the key
	generic(map[string][]string{}, key)
}
