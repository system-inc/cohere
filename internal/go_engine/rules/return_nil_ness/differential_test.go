package return_nil_ness

import (
	"path/filepath"
	"testing"

	"honnef.co/go/tools/analysis/facts/nilness"

	"github.com/system-inc/cohere/internal/go_engine/check/parity"
	"github.com/system-inc/cohere/internal/needs"
)

// factsDifferentialVariable names the tree the fact differential runs over: root for cohere's own module, or a
// directory, whose every package it loads as the engine does. The standard library is its source folder, under
// `GOROOT` set to the toolchain it belongs to.
const factsDifferentialVariable = "COHERE_GO_IR_FACTS_DIFFERENTIAL"

// The fact differential (#w7zmvat): over the whole tree COHERE_GO_IR_FACTS_DIFFERENTIAL names, and every package it
// imports, staticcheck's nilness on buildir and the port on the engine's own intermediate representation export the
// same facts on every package, the unexported functions' among them, failing on the first that differs. Unset, it
// skips, since a tree takes minutes.
func TestPortExportsTheOriginalsFactsOverATree(test *testing.T) {
	test.Parallel()
	directory := needs.OptIn(
		test,
		factsDifferentialVariable,
		"a differential of the port's facts against the original's over root or a directory",
	)
	if directory == "root" {
		// This package sits four directories below the module root.
		root, rootError := filepath.Abs(filepath.Join("..", "..", "..", ".."))
		if rootError != nil {
			test.Fatal(rootError)
		}
		directory = root
	}
	parity.RequireIdenticalCorpusFacts(test, nilness.Analysis, pushAnalyzer, directory)
}
