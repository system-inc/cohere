package program

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
)

// graduationProject builds a project of the named files, every one exporting something, and returns the graph and
// its project source files in the program's order.
func graduationProject(test *testing.T, names ...string) (*Graph, []*ast.SourceFile) {
	test.Helper()
	root := test.TempDir()
	configuration := `{"compilerOptions": {"target": "ES2022", "module": "esnext", "moduleResolution": "bundler", "strict": true, "noEmit": true}}`
	if writeError := os.WriteFile(
		filepath.Join(root, "tsconfig.json"),
		[]byte(configuration),
		0o644,
	); writeError != nil {
		test.Fatal(writeError)
	}
	for _, name := range names {
		text := "export function " + name + "(value: number): number {\n  return value + 1;\n}\n"
		if writeError := os.WriteFile(filepath.Join(root, name+".ts"), []byte(text), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
	graph, buildError := Build(Options{ConfigFileName: filepath.Join(root, "tsconfig.json")})
	if buildError != nil {
		test.Fatal(buildError)
	}
	var sources []*ast.SourceFile
	for _, sourceFile := range graph.ProjectFiles() {
		if !sourceFile.IsDeclarationFile && !ast.IsJsonSourceFile(sourceFile) {
			sources = append(sources, sourceFile)
		}
	}
	if len(sources) != len(names) {
		test.Fatalf("the program holds %d source files, want %d", len(sources), len(names))
	}
	return graph, sources
}

// A first run with no build info keys every shape on content (#5txm9gg). Graduating gives each the signature the
// up-front emit computes for the same bytes, so a graduated shape is the same shape a recorded one would be
// (#9knyr86).
func TestGraduationGivesEachContentKeyedShapeTheSignatureAnEmitComputes(test *testing.T) {
	test.Parallel()
	graph, sources := graduationProject(test, "alpha", "beta")
	backgroundContext := context.Background()
	shapes, _ := graph.Signatures(backgroundContext, nil)
	for _, sourceFile := range sources {
		if entry := shapes[sourceFile.FileName().AsString()]; entry.Signature != entry.Version {
			test.Fatalf(
				"%s already has a real signature before graduating, so this proves nothing",
				sourceFile.FileName(),
			)
		}
	}

	graduated, abandoned := graph.GraduateSignatures(shapes, time.Now().Add(time.Minute), 0)
	if graduated != len(sources) || abandoned != "" {
		test.Fatalf(
			"graduated %d of %d, abandoned %q; want every one and none abandoned",
			graduated,
			len(sources),
			abandoned,
		)
	}
	emitted, _ := graph.Signatures(backgroundContext, RecordedRealSignatures(graph))
	for _, sourceFile := range sources {
		entry := shapes[sourceFile.FileName().AsString()]
		if entry.Signature == entry.Version || entry.Abandoned {
			test.Errorf("%s did not graduate: %+v", sourceFile.FileName(), entry)
		}
		if want := emitted[sourceFile.FileName().AsString()]; entry != want {
			test.Errorf("%s graduated to %+v, and the up-front emit computes %+v", sourceFile.FileName(), entry, want)
		}
	}
}

// The deadline holds against an emit that never ends (#5txm9gg: one ESLint config ran 9 minutes). The file in
// progress at the deadline is abandoned, the files before it graduated, the files after it are left for the next
// run untouched, and an abandoned file is never emitted again while its bytes are the same.
//
// The deadline passes when the planted file's emit begins, said by the emit hook, never by the wall clock: a 200ms
// deadline once passed on Home, a loaded 64-core box, before the first file's emit finished (#3sz36vs).
//
// Not parallel: it sets beforeSignatureEmit, the package's emit hook.
func TestGraduationStopsAtItsDeadlineAndNeverRetriesTheFileItAbandoned(test *testing.T) {
	graph, sources := graduationProject(test, "alpha", "beta", "gamma")
	before, planted, after := sources[0].FileName(), sources[1].FileName(), sources[2].FileName()
	shapes, _ := graph.Signatures(context.Background(), nil)

	release := make(chan struct{})
	deadline := make(chan time.Time, 1)
	var deadlinePassed time.Time
	test.Cleanup(func() {
		close(release)
		beforeSignatureEmit = nil
	})
	beforeSignatureEmit = func(fileName string) {
		if fileName == planted.AsString() {
			deadlinePassed = time.Now()
			deadline <- deadlinePassed
			<-release
		}
	}
	graduated, abandoned := graph.graduateSignaturesUntil(shapes, deadline, 0)
	// Timing assertion: the planted file blocks until cleanup, so a return at all means the deadline stopped it,
	// and 5 seconds after it passed catches only a run that honored it late, never one slowed by load.
	if elapsed := time.Since(deadlinePassed); deadlinePassed.IsZero() || elapsed > 5*time.Second {
		test.Fatalf("graduating returned %s after the deadline passed, so the deadline did not stop it", elapsed)
	}
	if abandoned != planted.AsString() || graduated != 1 {
		test.Fatalf("graduated %d and abandoned %q; want 1 and the planted %s", graduated, abandoned, planted)
	}
	if entry := shapes[before.AsString()]; entry.Signature == entry.Version {
		test.Errorf("the file before the planted one did not graduate: %+v", entry)
	}
	if entry := shapes[planted.AsString()]; !entry.Abandoned || entry.Signature != entry.Version {
		test.Errorf("the planted file is not abandoned and content-keyed: %+v", entry)
	}
	if entry := shapes[after.AsString()]; entry.Abandoned || entry.Signature != entry.Version {
		test.Errorf("the file after the planted one was touched: %+v", entry)
	}

	// The next run tries what was left, and never the abandoned file.
	beforeSignatureEmit = func(fileName string) {
		if fileName == planted.AsString() {
			test.Errorf("the abandoned %s was emitted again with its bytes unchanged", planted)
		}
	}
	if graduated, abandoned := graph.GraduateSignatures(
		shapes,
		time.Now().Add(time.Minute),
		0,
	); graduated != 1 || abandoned != "" {
		test.Errorf("the next run graduated %d and abandoned %q; want the one file left and none", graduated, abandoned)
	}

	// Carried while its bytes are the same, and dropped once they change.
	carried, _ := graph.Signatures(context.Background(), shapes)
	if !carried[planted.AsString()].Abandoned {
		test.Errorf("the abandoned mark was dropped with the bytes unchanged: %+v", carried[planted.AsString()])
	}
	edited := map[string]SignatureEntry{}
	for name, entry := range shapes {
		edited[name] = entry
	}
	edited[planted.AsString()] = SignatureEntry{Version: "other bytes", Signature: "other bytes", Abandoned: true}
	fresh, _ := graph.Signatures(context.Background(), edited)
	if fresh[planted.AsString()].Abandoned {
		test.Errorf("the abandoned mark survived an edit to the file's bytes: %+v", fresh[planted.AsString()])
	}
}

// A file the deadline catches before its emit has run for abandonAfter is innocent: it is left keyed on content
// and unmarked, for the next run to try first, never abandoned for the bad luck of starting last. The deadline
// passes when the caught file's emit begins, said by the emit hook, as in the test above.
//
// Not parallel: it sets beforeSignatureEmit, the package's emit hook.
func TestGraduationLeavesAFileTheDeadlineCaughtEarlyForTheNextRun(test *testing.T) {
	graph, sources := graduationProject(test, "alpha", "beta")
	caught := sources[1].FileName()
	shapes, _ := graph.Signatures(context.Background(), nil)

	release := make(chan struct{})
	deadline := make(chan time.Time, 1)
	test.Cleanup(func() {
		close(release)
		beforeSignatureEmit = nil
	})
	beforeSignatureEmit = func(fileName string) {
		if fileName == caught.AsString() {
			deadline <- time.Now()
			<-release
		}
	}
	graduated, abandoned := graph.graduateSignaturesUntil(shapes, deadline, time.Hour)
	if graduated != 1 || abandoned != "" {
		test.Fatalf(
			"graduated %d and abandoned %q; want 1 and none, since the caught emit ran for less than abandonAfter",
			graduated,
			abandoned,
		)
	}
	if entry := shapes[caught.AsString()]; entry.Abandoned || entry.Signature != entry.Version {
		test.Errorf("the file caught early was not left as it was for the next run: %+v", entry)
	}
}
