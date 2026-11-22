package check

import (
	"crypto/sha256"
	"path/filepath"
	"slices"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules"
)

// bodiesFiles are the workspace the capture of U1000's records is read over: one package of two files, one a `//line`
// directive renames at its one function, so that body is keyed by the directive's name and parsed from the file's own.
var bodiesFiles = map[string]string{
	"go.mod": "module example.com/bodies\n\ngo 1.24\n",
	"plain.go": "// Package bodies holds a body keyed by its own file and one keyed by a directive's.\n" +
		"package bodies\n\nfunc Plain() int {\n\tvalue := 1\n\treturn value\n}\n",
	"lined.go": "package bodies\n\n//line other.go:40\nfunc Lined() int {\n\treturn Plain()\n}\n",
}

// loadBodies writes the bodies workspace into a directory of the test's own, loads it, and returns its one package,
// the directory and U1000's rule from the registry.
func loadBodies(test *testing.T) ([]*packages.Package, string, rule.Rule) {
	test.Helper()
	root := test.TempDir()
	writeFiles(test, root, bodiesFiles)
	loaded, loadError := load.Packages(root, []string{"./..."}, []string{"GOWORK=off"}, nil)
	if loadError != nil || len(loaded.Diagnostics) > 0 || len(loaded.Packages) != 1 {
		test.Fatalf("loading the bodies workspace: %v %+v", loadError, loaded)
	}
	var unused []rule.Rule
	for _, registered := range rules.All() {
		if registered.Name == rule.Namespace+"U1000" {
			unused = append(unused, registered)
		}
	}
	if len(unused) != 1 {
		test.Fatalf("the registry holds %d rules named U1000, want 1", len(unused))
	}
	return loaded.Packages, root, unused[0]
}

// Under Options.RecordBodies the capture holds what U1000's action recorded of each body (#xeafx79): Plain's keyed by
// its own file, Lined's by the `//line` directive's name and parsed from lined.go, each file's digest the sha256 of
// its bytes. A capture the run was not asked to record into holds none, though U1000 ran, and a run that captures no
// facts' reach is refused the ask, since nothing would hand its records back. reachNilnessAnalyzer runs beside U1000
// for ctrlflow, which the capture holds a package's side of only where its resolver learned it.
func TestTheCaptureTakesU1000sRecordsOnlyWhenAsked(test *testing.T) {
	test.Parallel()
	analyzed, root, unused := loadBodies(test)
	running := []rule.Rule{unused, houseRule(reachNilnessAnalyzer)}
	unasked, unaskedError := Run(analyzed, running, Options{FactReach: true})
	if unaskedError != nil {
		test.Fatalf("running unasked: %v", unaskedError)
	}
	if hosted := unasked.FactReach.Packages[analyzed[0].ID]; hosted == nil || len(hosted.Bodies.Bodies) != 0 ||
		len(hosted.Bodies.Digests) != 0 {
		test.Errorf("unasked, the capture holds %+v, want the package with no body recorded", hosted)
	}
	if _, refusal := Run(analyzed, running, Options{RecordBodies: true}); refusal == nil {
		test.Error("a run asked to record with no facts' reach captured ran")
	}

	asked, askedError := Run(analyzed, running, Options{FactReach: true, RecordBodies: true})
	if askedError != nil {
		test.Fatalf("running asked: %v", askedError)
	}
	hosted := asked.FactReach.Packages[analyzed[0].ID]
	if hosted == nil {
		test.Fatal("asked, the capture holds no side of the package")
	}
	type held struct {
		file       string
		offset     int
		parsedFile string
	}
	var got []held
	for _, body := range hosted.Bodies.Bodies {
		file, offset := body.Key()
		got = append(got, held{file: file, offset: offset, parsedFile: body.ParsedFile})
	}
	plain, lined := filepath.Join(root, "plain.go"), filepath.Join(root, "lined.go")
	// In the order the walk read them, the package's files sorted by name.
	want := []held{{file: filepath.Join(root, "other.go"), offset: 35, parsedFile: lined},
		{file: plain, offset: 101, parsedFile: plain}}
	if !slices.Equal(got, want) {
		test.Errorf("asked, the capture holds the bodies %+v, want %+v", got, want)
	}
	for name, path := range map[string]string{"plain.go": plain, "lined.go": lined} {
		if digest := hosted.Bodies.Digests[path]; digest != sha256.Sum256([]byte(bodiesFiles[name])) {
			test.Errorf("asked, the digest of %s is %x, want the sha256 of its bytes", name, digest)
		}
	}
}
