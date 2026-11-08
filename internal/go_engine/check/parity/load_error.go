package parity

import (
	"regexp"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/packages"
)

// LoadError is one error a fixture package loads with, as the loader reports it. Position is packages.Error.Pos, the
// file, line and column the loader prints, with the file named as Findings names it: its base name, after its
// module's pattern in a fixture module (see fixtureLoad), so `a.go:43:2`, and a position that names no file kept as
// the loader gives it. Message is packages.Error.Msg with each run of spaces, tabs and newlines made one space, since
// a type error can span lines, as a call's have and want do.
type LoadError struct {
	Position string
	Message  string
}

// String is the error as a failure names it, its position and then its message.
func (loadError LoadError) String() string {
	return loadError.Position + ": " + loadError.Message
}

// RequireIdenticalDespiteLoadErrors is RequireIdentical over fixture packages that fail to type-check on purpose, as
// some of x/tools' do for an analyzer that runs despite errors. The checker runs such a pair over what of each package
// type-checked, so the comparison counts, but only over packages that fail as the fixtures mean them to: their load
// errors must be expected exactly, as a multiset, and one that is not listed fails, as does one listed that the load
// does not report, each named by position and message (see LoadError). Then the findings are compared as
// RequireIdentical compares them, and an analyzer that did not run on a fixture package fails as it does there.
//
// It refuses a pair where either side does not run despite errors, since the checker would skip that side on every
// ill-typed package, and it refuses an empty list, since fixtures that load clean are RequireIdentical's, whose gate
// fails a fixture package with any error at all.
func RequireIdenticalDespiteLoadErrors(
	test testing.TB,
	original *analysis.Analyzer,
	port *analysis.Analyzer,
	testdata string,
	expected []LoadError,
	patterns ...string,
) {
	test.Helper()
	refused := false
	for _, side := range []struct {
		name     string
		analyzer *analysis.Analyzer
	}{{"original", original}, {"port", port}} {
		if !side.analyzer.RunDespiteErrors {
			test.Errorf("the %s, %s, does not run despite errors, so the checker would skip it on every ill-typed "+
				"fixture package", side.name, side.analyzer.Name)
			refused = true
		}
	}
	if len(expected) == 0 {
		test.Errorf("no load error is listed, and fixtures that load clean are RequireIdentical's")
		refused = true
	}
	if refused {
		return
	}
	requireIdentical(test, Target{}, original, port, testdata, expected, patterns...)
}

// positionPattern is a position as packages.Error.Pos gives one that names a file: the file, then its line and, for
// most errors, its column.
var positionPattern = regexp.MustCompile(`^(.+?)((?::\d+){1,2})$`)

// positionIn is position with its file named by fileName, or position as it is when it names no file, as a list
// error's `-` does.
func positionIn(position string, fileName func(string) string) string {
	match := positionPattern.FindStringSubmatch(position)
	if match == nil {
		return position
	}
	return fileName(match[1]) + match[2]
}

// loadErrorsOf is the errors roots, one load's, load with, in the order the loader reports them, each named as
// Findings names files. A test variant, `p [p.test]`, type-checks its package's files again and repeats their errors,
// so each error counts as many times as the one root that reports it most, and an error in a file a package and its
// test variant share counts once. Errors are told apart by the loader's own position, before fileName shortens it, so
// two packages' files of the same base name never merge.
func loadErrorsOf(roots []*packages.Package, fileName func(string) string) []LoadError {
	counted := map[LoadError]int{}
	loadErrors := []LoadError{}
	for _, root := range roots {
		inRoot := map[LoadError]int{}
		for _, reported := range root.Errors {
			asReported := LoadError{Position: reported.Pos, Message: strings.Join(strings.Fields(reported.Msg), " ")}
			inRoot[asReported]++
			if inRoot[asReported] <= counted[asReported] {
				continue
			}
			counted[asReported] = inRoot[asReported]
			loadErrors = append(loadErrors, LoadError{
				Position: positionIn(asReported.Position, fileName),
				Message:  asReported.Message,
			})
		}
	}
	return loadErrors
}

// requireLoadErrors fails the test unless loaded, the errors the fixture packages load with, are expected exactly,
// as a multiset, naming each one loaded that is not listed and each listed that did not load. With nothing expected
// it passes nothing loaded, which loadFixtures has already held clean.
func requireLoadErrors(test testing.TB, loaded []LoadError, expected []LoadError) {
	test.Helper()
	unlisted := loadErrorsWithout(loaded, expected)
	absent := loadErrorsWithout(expected, loaded)
	for _, loadError := range unlisted {
		test.Errorf("the fixture packages load with %s, which is not listed", loadError)
	}
	for _, loadError := range absent {
		test.Errorf("the list holds %s, and the fixture packages do not load with it", loadError)
	}
	if len(unlisted)+len(absent) > 0 {
		test.Fatalf("the fixture packages do not fail as listed, so their findings would be compared over packages " +
			"other than the ones meant")
	}
}

// loadErrorsWithout is what of from is not in without, counting repeats, in the order of from.
func loadErrorsWithout(from []LoadError, without []LoadError) []LoadError {
	remaining := map[LoadError]int{}
	for _, loadError := range without {
		remaining[loadError]++
	}
	missing := []LoadError{}
	for _, loadError := range from {
		if remaining[loadError] > 0 {
			remaining[loadError]--
			continue
		}
		missing = append(missing, loadError)
	}
	return missing
}
