// Package differences holds the cases where the port and the original could plausibly disagree, and records no
// difference, since there is none: both read the same calls. The one piece the port reads from elsewhere is
// staticcheck_report.DisplayPosition, the kit's port of staticcheck's, which writes where the finalizer starts into the
// message. So the finalizers here start after `//line` directives, in adjusted.go one naming a Go file, which both
// show, and in grammar.go one naming a grammar, which both pass over for the file as written, where a port that read
// the directive otherwise would show as a message one side makes and the other does not.
package differences

import "runtime"

func use(values ...any) {}

func capture(closure func()) {}

// Found by both: no directive, the message names this file. Each object here is captured by a closure before the
// call, so the IR keeps the call's load of it.
func plain() {
	object := new(int)
	capture(func() { use(object) })
	runtime.SetFinalizer(object, func(_ *int) { use(object) })
}
