package copy_lock

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/tools/go/analysis/passes/copylock"
	"golang.org/x/tools/txtar"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// x/tools' own fixtures for copylock, copied unchanged into testdata/src and run as x/tools' Test runs them, with no
// flags: every `// want` the original expects, the port reports at the same line with the same message, and
// analysistest fails on a finding no want expects as much as on a want nothing found, so the port adds nothing there
// either.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireUpstreamWants(test, Analyzer, analysistest.TestData(), "a", "typeparams", "issue67787", "unfortunate")
}

// x/tools' TestVersions22: a module at Go 1.22, whose files without a //go:build line declare their loop variables
// afresh on every iteration. x/tools extracts the archive with its internal testfiles, which the port's test cannot
// import, so extracted does the same with the public txtar package.
func TestPortMatchesUpstreamWantsAtGo122(test *testing.T) {
	test.Parallel()
	parity.RequireUpstreamWants(test, Analyzer, extracted(test, "forstmt/go22.txtar"), "golang.org/fake/forstmt")
}

// x/tools' TestVersions21: the same files in a module at Go 1.21, where only the one whose //go:build line names Go
// 1.22 declares its loop variables afresh.
func TestPortMatchesUpstreamWantsAtGo121(test *testing.T) {
	test.Parallel()
	parity.RequireUpstreamWants(test, Analyzer, extracted(test, "forstmt/go21.txtar"), "golang.org/fake/forstmt")
}

// fixture is one load the differential runs both analyzers over: a `GOPATH` tree, or a module extracted from the
// archive at archive under testdata/src, the packages named in it, and the errors they load with when any fail to
// type-check, as x/tools' own do.
type fixture struct {
	name       string
	archive    string
	patterns   []string
	loadErrors []parity.LoadError
}

// testdataLoadErrors are the errors the testdata load's fixture packages load with, 105 of them, each one a
// position below testdata/src and its message. x/tools' copylock runs despite errors, and its fixtures, copied
// unchanged, are written for what the analyzer reports rather than to type-check. In typeparams, the recursive type
// is the error its comment says the analyzer must survive, and issue67787's one error is the code that fixture holds
// for the analyzer to say nothing about. The rest are incidental to what the fixtures test. A Sizeof, Offsetof or
// Alignof's constant is a 64 bit target's, the one the test loads for, and typeparams' `~` error spells sync.Mutex's
// fields as the toolchain lays them out.
var testdataLoadErrors = []parity.LoadError{
	// a: copylock_func.go declares OkFunc and BadFunc three times over beside copylock.go's, and OkRet and BadRet
	// twice, each redeclaration its own error beside the declaration it repeats.
	{Position: "a/copylock_func.go:17:6", Message: "OkFunc redeclared in this block"},
	{Position: "a/copylock.go:11:6", Message: "other declaration of OkFunc"},
	{Position: "a/copylock_func.go:18:6", Message: "BadFunc redeclared in this block"},
	{Position: "a/copylock.go:46:6", Message: "other declaration of BadFunc"},
	{Position: "a/copylock_func.go:35:6", Message: "OkFunc redeclared in this block"},
	{Position: "a/copylock.go:11:6", Message: "other declaration of OkFunc"},
	{Position: "a/copylock_func.go:36:6", Message: "BadFunc redeclared in this block"},
	{Position: "a/copylock.go:46:6", Message: "other declaration of BadFunc"},
	{Position: "a/copylock_func.go:37:6", Message: "OkRet redeclared in this block"},
	{Position: "a/copylock_func.go:20:6", Message: "other declaration of OkRet"},
	{Position: "a/copylock_func.go:38:6", Message: "BadRet redeclared in this block"},
	{Position: "a/copylock_func.go:21:6", Message: "other declaration of BadRet"},
	{Position: "a/copylock_func.go:46:6", Message: "OkFunc redeclared in this block"},
	{Position: "a/copylock.go:11:6", Message: "other declaration of OkFunc"},
	{Position: "a/copylock_func.go:47:6", Message: "BadFunc redeclared in this block"},
	{Position: "a/copylock.go:46:6", Message: "other declaration of BadFunc"},
	// a: copylock_func.go's functions with a result and an empty body.
	{Position: "a/copylock_func.go:20:27", Message: "missing return"},
	{Position: "a/copylock_func.go:21:27", Message: "missing return"},
	{Position: "a/copylock_func.go:37:35", Message: "missing return"},
	{Position: "a/copylock_func.go:38:35", Message: "missing return"},
	{Position: "a/copylock_func.go:71:41", Message: "missing return"},
	// a: copylock_func.go's parameter typed by a local variable, and the misspelled names of its last function.
	{Position: "a/copylock_func.go:98:19", Message: "t (local variable) is not a type"},
	{Position: "a/copylock_func.go:122:7", Message: "undefined: EmbeddedRwMutex"},
	{Position: "a/copylock_func.go:124:7", Message: "undefined: OKRet"},
	{Position: "a/copylock_func.go:122:2", Message: "declared and not used: x"},
	// a: copylock.go's locals declared to be copied into and never read, and one `:=` that declares nothing new.
	{Position: "a/copylock.go:13:2", Message: "declared and not used: p"},
	{Position: "a/copylock.go:17:6", Message: "declared and not used: z"},
	{Position: "a/copylock.go:18:2", Message: "declared and not used: w"},
	{Position: "a/copylock.go:21:2", Message: "declared and not used: q"},
	{Position: "a/copylock.go:25:2", Message: "declared and not used: yy"},
	{Position: "a/copylock.go:32:2", Message: "declared and not used: nl"},
	{Position: "a/copylock.go:33:2", Message: "declared and not used: mx"},
	{Position: "a/copylock.go:34:2", Message: "declared and not used: xx"},
	{Position: "a/copylock.go:38:6", Message: "declared and not used: pz"},
	{Position: "a/copylock.go:39:2", Message: "declared and not used: pw"},
	{Position: "a/copylock.go:59:4", Message: "no new variables on left side of :="},
	{Position: "a/copylock.go:60:6", Message: "declared and not used: z"},
	{Position: "a/copylock.go:62:2", Message: "declared and not used: w"},
	{Position: "a/copylock.go:65:6", Message: "declared and not used: q"},
	{Position: "a/copylock.go:69:2", Message: "declared and not used: yy"},
	{Position: "a/copylock.go:82:2", Message: "declared and not used: muSlice"},
	{Position: "a/copylock.go:88:2", Message: "declared and not used: mmuSlice"},
	{Position: "a/copylock.go:94:2", Message: "declared and not used: fmuSlice"},
	{Position: "a/copylock.go:101:2", Message: "declared and not used: mumA"},
	{Position: "a/copylock.go:102:2", Message: "declared and not used: mumB"},
	{Position: "a/copylock.go:107:2", Message: "declared and not used: aLen"},
	{Position: "a/copylock.go:108:2", Message: "declared and not used: aCap"},
	// a: copylock.go's calls of unsafe's Sizeof, Offsetof and Alignof, each a constant left unused.
	{Position: "a/copylock.go:121:2", Message: "unsafe.Sizeof(mu) (constant 8 of type uintptr) is not used"},
	{Position: "a/copylock.go:122:2", Message: "unsafe1.Sizeof(mu) (constant 8 of type uintptr) is not used"},
	{Position: "a/copylock.go:123:2", Message: "Sizeof(mu) (constant 8 of type uintptr) is not used"},
	{Position: "a/copylock.go:135:2", Message: "unsafe.Offsetof(T{}.mu) (constant 8 of type uintptr) is not used"},
	{Position: "a/copylock.go:145:2", Message: "unsafe.Alignof(T{}.mu) (constant 4 of type uintptr) is not used"},
	// a: copylock.go's locals of each lock type, declared to be copied into and never read.
	{Position: "a/copylock.go:154:6", Message: "declared and not used: rwmuXX"},
	{Position: "a/copylock.go:155:2", Message: "declared and not used: rwmuX1"},
	{Position: "a/copylock.go:156:2", Message: "declared and not used: rwmuY"},
	{Position: "a/copylock.go:158:6", Message: "declared and not used: rwmuYY"},
	{Position: "a/copylock.go:159:2", Message: "declared and not used: rwmuP"},
	{Position: "a/copylock.go:160:2", Message: "declared and not used: rwmuZ"},
	{Position: "a/copylock.go:164:6", Message: "declared and not used: condXX"},
	{Position: "a/copylock.go:165:2", Message: "declared and not used: condX1"},
	{Position: "a/copylock.go:166:2", Message: "declared and not used: condY"},
	{Position: "a/copylock.go:168:6", Message: "declared and not used: condYY"},
	{Position: "a/copylock.go:169:2", Message: "declared and not used: condP"},
	{Position: "a/copylock.go:170:2", Message: "declared and not used: condZ"},
	{Position: "a/copylock.go:177:6", Message: "declared and not used: wgXX"},
	{Position: "a/copylock.go:178:2", Message: "declared and not used: wgX1"},
	{Position: "a/copylock.go:179:2", Message: "declared and not used: wgY"},
	{Position: "a/copylock.go:181:6", Message: "declared and not used: wgYY"},
	{Position: "a/copylock.go:182:2", Message: "declared and not used: wgP"},
	{Position: "a/copylock.go:183:2", Message: "declared and not used: wgZ"},
	{Position: "a/copylock.go:187:6", Message: "declared and not used: poolXX"},
	{Position: "a/copylock.go:188:2", Message: "declared and not used: poolX1"},
	{Position: "a/copylock.go:189:2", Message: "declared and not used: poolY"},
	{Position: "a/copylock.go:191:6", Message: "declared and not used: poolYY"},
	{Position: "a/copylock.go:192:2", Message: "declared and not used: poolP"},
	{Position: "a/copylock.go:193:2", Message: "declared and not used: poolZ"},
	{Position: "a/copylock.go:197:6", Message: "declared and not used: onceXX"},
	{Position: "a/copylock.go:198:2", Message: "declared and not used: onceX1"},
	{Position: "a/copylock.go:199:2", Message: "declared and not used: onceY"},
	{Position: "a/copylock.go:201:6", Message: "declared and not used: onceYY"},
	{Position: "a/copylock.go:202:2", Message: "declared and not used: onceP"},
	{Position: "a/copylock.go:203:2", Message: "declared and not used: onceZ"},
	{Position: "a/copylock.go:210:6", Message: "declared and not used: vXX"},
	{Position: "a/copylock.go:211:2", Message: "declared and not used: vX1"},
	{Position: "a/copylock.go:214:2", Message: "declared and not used: vY"},
	{Position: "a/copylock.go:216:6", Message: "declared and not used: vYY"},
	{Position: "a/copylock.go:217:2", Message: "declared and not used: vP"},
	{Position: "a/copylock.go:218:2", Message: "declared and not used: vZ"},
	{Position: "a/copylock.go:225:2", Message: "declared and not used: d"},
	{Position: "a/copylock.go:226:2", Message: "declared and not used: pd"},
	// a: copylock_range.go's range variables, declared to be copied into and never read.
	{Position: "a/copylock_range.go:13:6", Message: "declared and not used: mu"},
	{Position: "a/copylock_range.go:14:6", Message: "declared and not used: i"},
	{Position: "a/copylock_range.go:21:6", Message: "declared and not used: i"},
	{Position: "a/copylock_range.go:25:6", Message: "declared and not used: i"},
	{Position: "a/copylock_range.go:29:9", Message: "declared and not used: m"},
	{Position: "a/copylock_range.go:33:6", Message: "declared and not used: i"},
	{Position: "a/copylock_range.go:33:9", Message: "declared and not used: m"},
	{Position: "a/copylock_range.go:37:9", Message: "declared and not used: m"},
	{Position: "a/copylock_range.go:41:6", Message: "declared and not used: k"},
	{Position: "a/copylock_range.go:45:6", Message: "declared and not used: k"},
	{Position: "a/copylock_range.go:49:9", Message: "declared and not used: v"},
	{Position: "a/copylock_range.go:57:6", Message: "declared and not used: v"},
	// typeparams: a struct that holds itself, and `~` on sync.Mutex, whose underlying type is a struct.
	{Position: "typeparams/typeparams.go:11:6", Message: "invalid recursive type: R refers to itself"},
	{
		Position: "typeparams/typeparams.go:53:2",
		Message: `invalid use of ~ (underlying type of "sync".Mutex is struct{_ "sync".noCopy; ` +
			`mu "internal/sync".Mutex})`,
	},
	// issue67787: a type used as a value in a composite literal.
	{Position: "issue67787/issue67787.go:8:31", Message: "T (type) is not an expression"},
}

// fixtures are the loads the differential compares, each on its own, since the archives' files share base names.
var fixtures = []fixture{
	{
		name:       "testdata",
		patterns:   []string{"a", "typeparams", "issue67787", "unfortunate", "parity"},
		loadErrors: testdataLoadErrors,
	},
	{name: "forstmt/go22.txtar", archive: "forstmt/go22.txtar", patterns: []string{"golang.org/fake/forstmt"}},
	{name: "forstmt/go21.txtar", archive: "forstmt/go21.txtar", patterns: []string{"golang.org/fake/forstmt"}},
	{
		name:     "parity/patch_release.txtar",
		archive:  "parity/patch_release.txtar",
		patterns: []string{"golang.org/fake/versions"},
	},
	{
		name:     "parity/prerelease.txtar",
		archive:  "parity/prerelease.txtar",
		patterns: []string{"golang.org/fake/versions"},
	},
}

// Over x/tools' fixtures and parity's, the second holding every branch of the judgment and the shapes most likely to
// differ, the original and the port find the same findings, each the same in every field, in each load on its own.
// A load whose packages fail to type-check compares over them only when they fail exactly as listed.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	for _, loaded := range fixtures {
		test.Run(loaded.name, func(test *testing.T) {
			test.Parallel()
			directory := analysistest.TestData()
			if loaded.archive != "" {
				directory = extracted(test, loaded.archive)
			}
			if len(loaded.loadErrors) > 0 {
				parity.RequireIdenticalDespiteLoadErrors(test, copylock.Analyzer, Analyzer, directory,
					loaded.loadErrors, loaded.patterns...)
				return
			}
			parity.RequireIdentical(test, copylock.Analyzer, Analyzer, directory, loaded.patterns...)
		})
	}
}

// Over the differences fixture, code that does not type-check, the original panics (see the fixture's package doc),
// so it cannot run beside the port, and the port runs alone. Its wants are the findings the port makes where a value
// has a name, and analysistest fails on any other finding, so the port reports nothing for a value with none.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	analysistest.Run(errorsOnly{test}, analysistest.TestData(), Analyzer, "differences")
}

// extracted is the txtar archive at name under testdata/src, written into a directory of the test's own, which the
// test removes when it ends. x/tools' testfiles.ExtractTxtarFileToTmp.
func extracted(test *testing.T, name string) string {
	test.Helper()
	archive, parseError := txtar.ParseFile(filepath.Join(analysistest.TestData(), "src", name))
	if parseError != nil {
		test.Fatal(parseError)
	}
	files, filesError := txtar.FS(archive)
	if filesError != nil {
		test.Fatal(filesError)
	}
	directory := test.TempDir()
	if copyError := os.CopyFS(directory, files); copyError != nil {
		test.Fatal(copyError)
	}
	return directory
}

// errorsOnly hands analysistest.Run a reporter that isn't a testing.TB. Given a testing.TB, Run first proves the go
// command works by building a throwaway program, and a runner of prebuilt test binaries refuses a test that builds.
// Loading the fixtures needs only go list.
type errorsOnly struct{ test *testing.T }

func (reporter errorsOnly) Errorf(format string, arguments ...any) {
	reporter.test.Helper()
	reporter.test.Errorf(format, arguments...)
}
