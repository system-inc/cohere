package check

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules"
)

// listedFact is the fact listingAnalyzer learns, of nothing: it lists the facts it inherited, and learns none.
type listedFact struct{}

// AFact marks listedFact a fact.
func (*listedFact) AFact() {
	// A fact's method is its mark alone, as analysis.Fact asks, and does nothing.
}

// listingAnalyzer lists every object fact and package fact its action inherited, as Pass.AllObjectFacts and
// Pass.AllPackageFacts give them, and reports nothing.
var listingAnalyzer = &analysis.Analyzer{
	Name:      "listing",
	Doc:       "listing lists every fact it inherited",
	FactTypes: []analysis.Fact{(*listedFact)(nil)},
	Run: func(pass *analysis.Pass) (any, error) {
		pass.AllObjectFacts()
		pass.AllPackageFacts()
		return nil, nil
	},
}

// listsEveryFact reports whether reads were marked never by a listing of every fact (checker's markFactListing).
func listsEveryFact(never string) bool {
	return strings.Contains(never, "lists every fact of its import closure")
}

// A listing of every fact an action inherited reads the whole import closure, which no read names: an analyzer that
// lists them marks each package it runs on never, so the verdict declines it in every build, and the walk takes its
// set as the whole closure.
func TestAFactListingReadsTheWholeClosure(test *testing.T) {
	test.Parallel()

	loaded, byPath := loadedBuildReads(test)
	if _, runError := Run(loaded.Packages, []rule.Rule{houseRule(listingAnalyzer)}, Options{Reads: loaded.Reads,
		WalkSlots: loaded.WalkSlots}); runError != nil {
		test.Fatal(runError)
	}
	for path, analyzed := range byPath {
		reads := loaded.Reads[analyzed]
		if !listsEveryFact(reads.Never()) {
			test.Errorf("%s's reads are never %q, want marked by its listing of every fact", path, reads.Never())
		}
		if set := NewReadsBuild().Dependence(restoringOf(analyzed, reads)); !set.WholeClosure {
			test.Errorf("%s's dependence set is %v, want the whole closure", path, dependenceNames(set))
		}
	}
}

// The deprecated facts analyzer is the one registered analyzer whose listing is read, one recorded question at a time
// by deprecated_use, the one rule that reads its result (listingsRead): every registered rule run over the fixture
// marks no package by a listing, and no rule's source but deprecated_use's imports the deprecated facts, where a rule
// would take their result. The rules are dispatched, so every one of them requires every listener's requirements, and
// only their source tells which reads the result.
func TestOnlyTheDeprecatedFactsAreListed(test *testing.T) {
	test.Parallel()

	loaded, byPath := loadedBuildReads(test)
	if _, runError := Run(loaded.Packages, rules.All(), Options{Reads: loaded.Reads,
		WalkSlots: loaded.WalkSlots}); runError != nil {
		test.Fatal(runError)
	}
	for path, analyzed := range byPath {
		if never := loaded.Reads[analyzed].Never(); listsEveryFact(never) {
			test.Errorf("%s's reads are never %q: a registered analyzer lists every fact, and its reads are not "+
				"recorded", path, never)
		}
	}
	var readers []string
	walkError := filepath.WalkDir(filepath.Join(
		"..",
		"rules",
	), func(path string, entry fs.DirEntry, walkError error) error {
		if walkError != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return walkError
		}
		source, readError := os.ReadFile(path)
		if readError != nil {
			return readError
		}
		if strings.Contains(string(source), `"honnef.co/go/tools/analysis/facts/deprecated"`) {
			readers = append(readers, filepath.ToSlash(path))
		}
		return nil
	})
	if walkError != nil {
		test.Fatal(walkError)
	}
	if !slices.Equal(readers, []string{"../rules/deprecated_use/analyzer.go"}) {
		test.Errorf("the rules importing the deprecated facts are %v, want deprecated_use's alone", readers)
	}
}
