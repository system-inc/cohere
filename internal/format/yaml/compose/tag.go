package compose

import "regexp"

// Tag is a schema tag, upstream's ScalarTag or CollectionTag.
type Tag struct {
	Tag string
	// Collection is "map" or "seq" for a collection tag, "" for a scalar tag (undefined).
	Collection string
	Default    tagDefault
	// Test is the tag's test regular expression, nil when it has none. It is matched against the
	// asciiView of the value, which every test's ASCII-only classes read the same way.
	Test   *regexp.Regexp
	Format string
	// NodeClass is the collection class the tag constructs, "" when it has none.
	NodeClass string

	// resolveScalar is a scalar tag's resolve. It returns a *Node when upstream's returns a Scalar,
	// otherwise the raw value, and an error where upstream's throws.
	resolveScalar func(value []uint16, onError func(message string), options *Options) (any, error)
	// resolveCollection is a collection tag's resolve.
	resolveCollection func(collection *Node, onError func(message string)) *Node

	// prototypeProperty marks what knownTag finds for a tag named like a property every JavaScript
	// object inherits: something with no tag, collection, test or resolve.
	prototypeProperty bool
}

// test is `tag.test?.test(value)`.
func (tag *Tag) test(value []uint16) bool {
	return tag.Test != nil && tag.Test.MatchString(asciiView(value))
}

// tagString is `${tag.tag}`: "undefined" for an inherited property, which has no tag.
func (tag *Tag) tagString() string {
	if tag.prototypeProperty {
		return "undefined"
	}
	return tag.Tag
}
