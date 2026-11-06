package selector

// namespacePrefix is the namespace a word, universal or attribute carries, in upstream's
// `this.prevToken && this.prevToken[1] || true`: the text before the bar, or bare (namespace true) for a
// bar with nothing before it. The zero value is no namespace, undefined upstream.
type namespacePrefix struct {
	text string
	bare bool
}

// present is upstream's `if (namespace)`: true and a non-empty text are truthy, and a namespace that is
// not bare always has text.
func (namespace namespacePrefix) present() bool {
	return namespace.bare || namespace.text != ""
}
