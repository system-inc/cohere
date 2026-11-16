// Package split declares one function every build compiles and one each build declares in a file of its own.
package split

// Shared is the same code in every build, its parameter's answer the same object's in each.
func Shared(dst []byte) []byte { return append(dst, 's') }

// Serve's second parameter is an interface of its own, whose method, and that method's parameter, Serve's declaration
// declares beside Serve.
func Serve(dst []byte, handler interface{ Handle(buf []byte) }) { handler.Handle(dst) }
