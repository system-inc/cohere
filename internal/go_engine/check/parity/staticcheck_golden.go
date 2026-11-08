package parity

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"go/format"
	"io/fs"
	"maps"
	"os"
	"slices"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/txtar"
)

// RequireStaticcheckGolden runs port over staticcheck's own fixtures in testdata, copied unchanged with their golden
// files, and its fixes must make each golden file, as staticcheck's testutil (analysis/lint/testutil/check.go)
// requires of the original's. A fixture file's golden file sits beside it, named for it with .golden added, and is a
// txtar archive: with sections, each is named for a fix's message and holds the file with every edit of the fixes of
// that message applied, every message needs its section and every section its message, and a comment beside them is
// refused; with none, its comment is the file with every edit applied. The fixed file is gofmt'd before it is
// compared, and a section's trailing newlines are cut to one. Each package is held apart, as testutil holds it, so a
// file a package and its test variant share takes each one's fixes once.
//
// testutil holds only the files a fix edits. Here every golden file of a fixture package is held, so a port that
// offers no fix where the original's golden file holds one fails. A fix in a file with no golden file fails, as it
// does there, and so do fixtures with no golden file at all, since the comparison would prove nothing. It loads for
// the machine's own target, and Target.RequireStaticcheckGolden for another.
func RequireStaticcheckGolden(test testing.TB, port *analysis.Analyzer, testdata string, patterns ...string) {
	test.Helper()
	requireStaticcheckGolden(test, Target{}, port, testdata, patterns...)
}

// requireStaticcheckGolden is RequireStaticcheckGolden with the fixtures loaded for target.
func requireStaticcheckGolden(
	test testing.TB,
	target Target,
	port *analysis.Analyzer,
	testdata string,
	patterns ...string,
) {
	test.Helper()
	suggested := 0
	held := 0
	for _, fixtures := range loadFixtures(test, target, testdata, patterns) {
		for _, action := range portActions(test, port, fixtures.roots) {
			// editsByFile is the package's edits by the file they edit, and in each by their fix's message.
			editsByFile := map[string]map[string][]Edit{}
			for _, diagnostic := range action.Diagnostics {
				for _, fix := range newFinding(action.Package.Fset, diagnostic).Fixes {
					suggested++
					for _, edit := range fix.Edits {
						if editsByFile[edit.File] == nil {
							editsByFile[edit.File] = map[string][]Edit{}
						}
						editsByFile[edit.File][fix.Message] = append(editsByFile[edit.File][fix.Message], edit)
					}
				}
			}
			// The package's files, and any other a fix edits, so an edit outside them still needs a golden file.
			files := slices.Clone(action.Package.GoFiles)
			for _, edited := range slices.Sorted(maps.Keys(editsByFile)) {
				if !slices.Contains(files, edited) {
					files = append(files, edited)
				}
			}
			for _, file := range files {
				golden, readError := os.ReadFile(file + ".golden")
				if errors.Is(readError, fs.ErrNotExist) {
					if len(editsByFile[file]) > 0 {
						test.Errorf("%s: the port's fixes in %s edit it, and it has no golden file",
							fixtures.fileName(file), action.Package.ID)
					}
					continue
				}
				if readError != nil {
					test.Fatalf("reading the golden file of %s: %v", fixtures.fileName(file), readError)
				}
				held++
				requireGolden(test, goldenFile{
					name:        fixtures.fileName(file) + ".golden",
					packageName: action.Package.ID,
					source:      file,
					archive:     txtar.Parse(golden),
				}, editsByFile[file])
			}
		}
	}
	test.Logf("the port suggested %d fixes, and %d golden files were held to them", suggested, held)
	if held == 0 {
		test.Fatalf("the fixtures hold no golden file, so the comparison proves nothing")
	}
}

// goldenFile is one golden file as a package holds it: its name as failures say it, the package, the path of the
// fixture file it is the golden file of, and its archive.
type goldenFile struct {
	name        string
	packageName string
	source      string
	archive     *txtar.Archive
}

// requireGolden fails the test unless editsByMessage, the package's edits of the golden file's fixture file by their
// fix's message, make the golden file.
func requireGolden(test testing.TB, golden goldenFile, editsByMessage map[string][]Edit) {
	test.Helper()
	source, readError := os.ReadFile(golden.source)
	if readError != nil {
		test.Fatalf("reading the fixture file of %s: %v", golden.name, readError)
	}
	messages := slices.Sorted(maps.Keys(editsByMessage))
	if len(golden.archive.Files) == 0 {
		every := []Edit{}
		for _, message := range messages {
			every = append(every, editsByMessage[message]...)
		}
		if fixed, isFixed := applied(test, golden, source, every); isFixed {
			compareGolden(test, golden, golden.name, fixed, golden.archive.Comment)
		}
		return
	}
	if len(golden.archive.Comment) != 0 {
		test.Errorf("%s holds sections and a comment beside them, which staticcheck's testutil refuses", golden.name)
		return
	}
	for _, message := range messages {
		index := slices.IndexFunc(golden.archive.Files, func(section txtar.File) bool {
			return section.Name == message
		})
		if index < 0 {
			test.Errorf("%s holds no section for the fix %q the port suggests in %s", golden.name, message,
				golden.packageName)
			continue
		}
		if fixed, isFixed := applied(test, golden, source, editsByMessage[message]); isFixed {
			compareGolden(test, golden, fmt.Sprintf("the section %q of %s", message, golden.name), fixed,
				append(bytes.TrimRight(golden.archive.Files[index].Data, "\n"), '\n'))
		}
	}
	for _, section := range golden.archive.Files {
		if _, isSuggested := editsByMessage[section.Name]; !isSuggested {
			test.Errorf("%s holds a section for the fix %q, which the port never suggests in %s", golden.name,
				section.Name, golden.packageName)
		}
	}
}

// applied is source, the fixture file of golden, with edits applied, gofmt'd as staticcheck's testutil formats it,
// and whether it could be: edits that overlap, or that make source gofmt cannot format, fail the test.
func applied(test testing.TB, golden goldenFile, source []byte, edits []Edit) ([]byte, bool) {
	test.Helper()
	ordered := slices.Clone(edits)
	// Applied from the last to the first, each edit's offsets still name the bytes they were read against, and of two
	// at one offset the one that replaces comes first, so an insertion there lands before what replaced the rest, as
	// testutil applies them from the first.
	slices.SortFunc(ordered, func(first Edit, second Edit) int {
		return cmp.Or(cmp.Compare(second.Start, first.Start), cmp.Compare(second.End, first.End))
	})
	fixed := slices.Clone(source)
	for index, edit := range ordered {
		if index > 0 && edit.End > ordered[index-1].Start {
			test.Errorf("the port's fixes in %s overlap at offset %d of the fixture file of %s", golden.packageName,
				edit.Start, golden.name)
			return nil, false
		}
		fixed = slices.Concat(fixed[:edit.Start], []byte(edit.NewText), fixed[edit.End:])
	}
	formatted, formatError := format.Source(fixed)
	if formatError != nil {
		test.Errorf("the port's fixes in %s make source gofmt cannot format, for %s: %v\n%s", golden.packageName,
			golden.name, formatError, fixed)
		return nil, false
	}
	return formatted, true
}

// compareGolden fails the test unless fixed, the port's fixes applied, is expected, what the golden file's part named
// where holds.
func compareGolden(test testing.TB, golden goldenFile, where string, fixed []byte, expected []byte) {
	test.Helper()
	if !bytes.Equal(fixed, expected) {
		test.Errorf("the port's fixes in %s make\n%s\nwhere %s holds\n%s", golden.packageName, fixed, where, expected)
	}
}
