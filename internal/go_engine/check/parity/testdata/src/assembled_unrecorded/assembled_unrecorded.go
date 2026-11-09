// Package assembled_unrecorded is assembled with the record in its assembly removed, so the call there that the port
// alone finds is unrecorded.
package assembled_unrecorded

func gained() {}

// gains holds the planted record, ruled in this doc comment: the port rightly finds gained.
func gains() {
	gained() // port finds: call to gained
}

func calls()
