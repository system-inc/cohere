package duplicate_functions

import (
	"crypto/sha256"
	"encoding/binary"
)

// DigestBuilder builds one Digest from tagged parts. Every part is prefixed by its kind and, for text, its length, so
// no two lists of parts write the same bytes: `"ab","c"` and `"a","bc"` differ, as do a number and a text that
// spells it. Both prototypes on #6rj7jw7 wrote this twice; this is its one home.
type DigestBuilder struct {
	bytes []byte
}

// NewDigestBuilder starts a digest whose first part is tag, the kind of value it names.
func NewDigestBuilder(tag string) *DigestBuilder {
	builder := &DigestBuilder{}
	return builder.Text(tag)
}

// Text adds a string.
func (builder *DigestBuilder) Text(value string) *DigestBuilder {
	builder.bytes = append(builder.bytes, 't')
	builder.bytes = binary.AppendUvarint(builder.bytes, uint64(len(value)))
	builder.bytes = append(builder.bytes, value...)
	return builder
}

// Number adds an integer.
func (builder *DigestBuilder) Number(value int) *DigestBuilder {
	builder.bytes = append(builder.bytes, 'n')
	builder.bytes = binary.AppendVarint(builder.bytes, int64(value))
	return builder
}

// Flag adds a boolean.
func (builder *DigestBuilder) Flag(value bool) *DigestBuilder {
	if value {
		builder.bytes = append(builder.bytes, 'T')
		return builder
	}
	builder.bytes = append(builder.bytes, 'F')
	return builder
}

// Digest adds another digest.
func (builder *DigestBuilder) Digest(value Digest) *DigestBuilder {
	builder.bytes = append(builder.bytes, 'd')
	builder.bytes = append(builder.bytes, value[:]...)
	return builder
}

// Sum is the digest of every part added so far. The builder can go on taking parts after it.
func (builder *DigestBuilder) Sum() Digest {
	full := sha256.Sum256(builder.bytes)
	var result Digest
	copy(result[:], full[:len(result)])
	return result
}
