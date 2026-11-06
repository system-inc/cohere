// Package flagged declares an abbreviated name of every kind Go has, in every form the vocabulary judges.
package flagged

import (
	"context"
	"foreign"
	"sort"
	"time"
)

// A package-level constant, in the prefix form.
const maxRetries = 3 // want `Identifier "maxRetries" should not be abbreviated\. Use "maximumRetries"\.`

// A package-level variable, the whole word with its expansion or a more descriptive name.
var cfg = map[string]string{} // want `Identifier "cfg" should not be abbreviated\. Use "configuration" or a more descriptive name\.`

// An exported function, the word as a segment, since Go's capital opens a word as camelCase does.
func ParseCfg() {} // want `Identifier "ParseCfg" abbreviates "Cfg"\. Use "ParseConfiguration"\.`

// A type, in the prefix form.
type srcFile struct { // want `Identifier "srcFile" should not be abbreviated\. Use "sourceFile"\.`
	// A field whose tag carries the external spelling is judged by its Go name all the same.
	Src string `json:"src"` // want `Identifier "Src" abbreviates "Src"\. Use "Source"\.`
	// The suffix form.
	ownerRef string // want `Identifier "ownerRef" should not end with "Ref"\. Use "ownerReference"\.`
}

// A method; a parameter whose bare expansion is a package this one imports; a named result whose bare expansion is
// predeclared. Both get the descriptive advice instead of the expansion.
func (file *srcFile) readBuf( // want `Identifier "readBuf" abbreviates "Buf"\. Use "readBuffer"\.`
	ctx context.Context, // want `Identifier "ctx" should not be abbreviated\. Use a descriptive name ending in "Context", since "context" is already Go's name for something else\.`
) (err error) { // want `Identifier "err" should not be abbreviated\. Use a descriptive name ending in "Error", since "error" is already Go's name for something else\.`
	return ctx.Err()
}

// A receiver.
func (cfgFile *srcFile) describe() string { // want `Identifier "cfgFile" should not be abbreviated\. Use "configurationFile"\.`
	return cfgFile.ownerRef
}

// A method named like sort.Interface's Len with another signature implements nothing, so its name is ours.
func (file *srcFile) Len() string { // want `Identifier "Len" abbreviates "Len"\. Use "Length"\.`
	return file.Src
}

// A Len with sort.Interface's exact signature, on a type that implements none of the rest, is ours all the same: the
// name is fixed only when the whole interface is implemented.
type halfSorter []int

func (list halfSorter) Len() int { return len(list) } // want `Identifier "Len" abbreviates "Len"\. Use "Length"\.`

var _ = sort.Ints

// An interface method of ours that no other package's method matches.
type sizer interface {
	bufLen() int // want `Identifier "bufLen" should not be abbreviated\. Use "bufferLen"\.`
}

// A type parameter, in the suffix form.
func first[Res any](values []Res) Res { // want `Identifier "Res" should not end with "Res"\. Use "Response"\.`
	return values[0]
}

// A label, range variables, a type switch variable, a local constant, and local variables in the advice forms.
func walk(values []any) {
idxLoop: // want `Identifier "idxLoop" should not be abbreviated\. Use "indexLoop"\.`
	for idx, val := range values { // want `Identifier "idx" should not be abbreviated\. Use "index" or a more descriptive name\.` `Identifier "val" should not be abbreviated\. Use "value" or a more descriptive name\.`
		switch str := val.(type) { // want `Identifier "str" should not be abbreviated\. Use a descriptive name ending in "String", since "string" is already Go's name for something else\.`
		case string:
			_ = str
			continue idxLoop
		}
		_ = idx
	}
	const timeoutMs = 5 // want `Identifier "timeoutMs" should not abbreviate milliseconds as "Ms"\. Use "timeoutInMilliseconds"`
	fn := func() {}     // want `Identifier "fn" should not be abbreviated\. Use "callback", "handler", "factory", or a more descriptive name\.`
	fn()
	argCount := len(values) // want `Identifier "arg" should not be abbreviated\. Use "argument", "commandArgument", or a more descriptive name\.`
	_ = argCount
	_ = time.Duration(timeoutMs)
}

// A Cmd with foreign.Commander's exact signature, on a type that implements none of the rest, is ours.
type halfShell struct{}

func (halfShell) Cmd() string { return "" } // want `Identifier "Cmd" abbreviates "Cmd"\. Use "Command"\.`

var _ foreign.Commander

// A Src with the shape of foreign.Source's, on a type that implements none of the rest of Source[int], is ours.
type halfReader struct{}

func (halfReader) Src() int { return 0 } // want `Identifier "Src" abbreviates "Src"\. Use "Source"\.`

// A Src that shares only the name of foreign.Source's, with another shape, implements no instance of it.
type wideReader struct{}

func (wideReader) Src(name string) int { return 0 } // want `Identifier "Src" abbreviates "Src"\. Use "Source"\.`
