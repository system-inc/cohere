// Package go_listing is what `go list -json` says of a build's packages, read once for the programs
// that each read it their own way today. In cohere: internal/build_inputs (the files a test's go build
// reads), internal/go_types (list.go's listing and product.go's key and export build), the release
// dispatch's `--dev` hash, and the go_engine load's listings. In Adamic: internal/buildcache's gobuild.go
// (keyInputs), settle.go (recipeEntries) and make_go.go, and internal/lowered. In Loom: the planner's
// closure, its cold reads, executed sources and phases, the resident builder and hot's closures.
//
// It holds what go list says and which files that names, in two layers. The first is pure, over bytes:
// Decode reads go list's stream of objects, and Reach, ReachError, SourceFiles and
// CgoIncludeDirectories answer from what it read. The second is a thin runner: Arguments spells go
// list's flags, and Run runs a command the caller built and decodes its output. It never builds a
// command itself, so which go runs, in which directory and under which environment stays each
// consumer's.
//
// It holds no key, no hash, no name scheme for a file and no go.sum or go.work policy: those differ
// between the consumers by design. A package's Error and DepsErrors come through as go list printed
// them, and only a caller that asks (ReachError) refuses on them.
//
// It is a module of its own, beside git_tree, so Loom and Adamic can require it without requiring
// cohere's root module.
package go_listing

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// A Module is the module a listed package belongs to.
type Module struct {
	Path      string
	Version   string
	Directory string `json:"Dir"`
	GoMod     string
	GoVersion string
	// Main is whether the module is a main module, one the go command was run in rather than a
	// dependency.
	Main    bool
	Replace *Module
}

// Decode reads go list's -json output, a stream of objects, into its packages in go's order:
// dependencies first under -deps. The empty output is no packages. Anything that isn't a stream of
// objects, a value of another type or one cut short, is refused, named by its place in the stream.
// An object with no ImportPath is read as it stands, since a -json field list may leave it out.
func Decode(output []byte) ([]Package, error) {
	var packages []Package
	decoder := json.NewDecoder(bytes.NewReader(output))
	for {
		// Each value is taken whole before it is read as a Package, since a null would read as a
		// package with nothing in it.
		var value json.RawMessage
		decodeError := decoder.Decode(&value)
		if errors.Is(decodeError, io.EOF) {
			return packages, nil
		}
		if decodeError != nil {
			return nil, fmt.Errorf("reading go list's output, object %d: %w", len(packages), decodeError)
		}
		if value[0] != '{' {
			return nil, fmt.Errorf("reading go list's output, object %d: %.40q isn't an object", len(packages), value)
		}
		var listed Package
		if unmarshalError := json.Unmarshal(value, &listed); unmarshalError != nil {
			return nil, fmt.Errorf("reading go list's output, object %d: %w", len(packages), unmarshalError)
		}
		packages = append(packages, listed)
	}
}
