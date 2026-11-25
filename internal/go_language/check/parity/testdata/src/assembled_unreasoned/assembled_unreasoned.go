// Package assembled_unreasoned holds a record in its assembly with no reason on the line above it, which a file with
// no declarations cannot give in a doc comment.
package assembled_unreasoned

func gained() {}

// gains holds the planted record, ruled in this doc comment: the port rightly finds gained.
func gains() {
	gained() // port finds: call to gained
}

func calls()
