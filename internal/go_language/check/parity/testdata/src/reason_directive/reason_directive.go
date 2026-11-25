// Package reason_directive holds two records whose only comment above is a directive, which explains nothing, so
// neither has a reason.
package reason_directive

func gained() {}

func calls() {
	//go:generate echo
	gained() // port finds: call to gained
	//cohere:ignore
	gained() // port finds: call to gained
}
