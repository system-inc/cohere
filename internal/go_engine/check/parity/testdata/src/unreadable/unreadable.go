// Package unreadable holds a record in no grammar the harness reads.
package unreadable

func gained() {}

func calls() {
	gained() // port found: call to gained
}
