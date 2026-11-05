package allowed

import _ "unsafe"

// cgo writes an exported function into a C header under the name its directive gives, the C side's spelling.
//
//export callback_from_c
func callback_from_c() {}

// The linker binds this name to another package's symbol, so it is spelled as the directive needs.
//
//go:linkname runtime_nano runtime.nanotime
func runtime_nano() int64
