// Package assembled holds a call only the port finds in Go, recorded in this file, and one in its assembly, recorded
// in assembled.s.
package assembled

func gained() {}

// gains holds the planted record, ruled in this doc comment: the port rightly finds gained.
func gains() {
	gained() // port finds: call to gained
}

func calls()
