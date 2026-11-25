// Package inner is embedded from the directory above, so the message names a repeat's other file by a path down
// into this one, or up out of it.
package inner

// InnerLeaf claims a json name parity's Leaf claims.
type InnerLeaf struct {
	Name string `json:"name"`
}

// Middle puts InnerLeaf one level further down.
type Middle struct {
	InnerLeaf
}

// Hiding claims a name with an unexported field, which an embedding does not look at.
type Hiding struct {
	name string `json:"name"`
}

// Repeats repeats a name within this package, to be reported here.
type Repeats struct {
	InnerLeaf
	Again string    `json:"name"`
	Twice InnerLeaf `json:"twice"`
	Once  InnerLeaf `json:"twice"`
}
