// Package sibling is dot imported from a directory beside it, so the message names a repeat's other file by a path
// up and across.
package sibling

// SiblingLeaf claims a json name parity's Leaf claims.
type SiblingLeaf struct {
	Name string `json:"name"`
}
