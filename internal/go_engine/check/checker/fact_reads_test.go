package checker

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/build_reads"
	"github.com/system-inc/cohere/internal/go_engine/load"
)

// repeatedAsks is how many times repeatingAnalyzer asks the same fact of another package's function.
const repeatedAsks = 1000

// repeatingAnalyzer asks, on every package that imports one, the declaredFact of the first function its first import
// declares, repeatedAsks times over, as ctrlflow asks of a callee at every call.
var repeatingAnalyzer = &analysis.Analyzer{
	Name:      "repeating",
	Doc:       "repeating asks one fact of another package many times",
	FactTypes: []analysis.Fact{new(declaredFact)},
	Run: func(pass *analysis.Pass) (any, error) {
		imported := pass.Pkg.Imports()
		if len(imported) == 0 {
			return nil, nil
		}
		asked := imported[0].Scope().Lookup("Base")
		for range repeatedAsks {
			pass.ImportObjectFact(asked, new(declaredFact))
		}
		return nil, nil
	},
}

// An action that asks the same fact of another package a thousand times gathers it once and publishes it to its
// package's reads once, under one lock, as its run ends, never once a read.
func TestAnActionPublishesItsFactReadsOnce(test *testing.T) {
	test.Parallel()

	directory := test.TempDir()
	for name, contents := range map[string]string{
		"go.mod":       "module example.com/asks\n\ngo 1.21\n",
		"base/base.go": "package base\n\n// Base is one.\nfunc Base() int { return 1 }\n",
		"user/user.go": "package user\n\nimport \"example.com/asks/base\"\n\n// User is Base.\n" +
			"func User() int { return base.Base() }\n",
	} {
		path := filepath.Join(directory, filepath.FromSlash(name))
		if directoryError := os.MkdirAll(filepath.Dir(path), 0o755); directoryError != nil {
			test.Fatal(directoryError)
		}
		if writeError := os.WriteFile(path, []byte(contents), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
	loaded, loadError := load.Packages(directory, []string{"./user"}, []string{"GOWORK=off"}, nil)
	if loadError != nil || len(loaded.Roots) != 1 {
		test.Fatalf("loading the fixture: %v", loadError)
	}
	user := loaded.Roots[0]
	reads := map[*packages.Package]*build_reads.Reads{user: {}}
	var publishedLock sync.Mutex
	var published []int
	options := &Options{Reads: reads, publishedFacts: func(publishedPackage *packages.Package, count int) {
		if publishedPackage == user {
			publishedLock.Lock()
			defer publishedLock.Unlock()
			published = append(published, count)
		}
	}}
	if _, analyzeError := Analyze([]*analysis.Analyzer{repeatingAnalyzer}, loaded.Roots, options); analyzeError != nil {
		test.Fatal(analyzeError)
	}
	if len(published) != 1 || published[0] != 1 {
		test.Fatalf("want user's one fact read published once, got publishes of %v", published)
	}
	if facts := reads[user].Facts(); len(facts) != 1 || facts[0].Object.Name() != "Base" || facts[0].Found {
		test.Fatalf("want user's one read, base.Base's missing fact, got %+v", facts)
	}
}
