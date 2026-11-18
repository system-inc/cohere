package go_engine

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/cache"
	"github.com/system-inc/cohere/internal/go_engine/contract"
	"github.com/system-inc/cohere/internal/go_engine/rules"
)

// The host records what each package reads of others only when the run has an extra build to restore packages into
// (#b2t9xmf): under `--other-builds`, a module the host builds whole records nothing, and the same module with a file
// only another platform builds records every package its load reaches.
func TestTheHostRecordsReadsOnlyWithAnExtraBuild(test *testing.T) {
	test.Parallel()

	// A platform other than the host's, whose file the host leaves out and an extra build compiles.
	other := "linux"
	if runtime.GOOS == other {
		other = "windows"
	}
	for _, withExtra := range []bool{false, true} {
		root := test.TempDir()
		files := map[string]string{
			"go.mod":       "module example.com/records\n\ngo 1.21\n",
			"lone/lone.go": "package lone\n\n// Lone is one.\nconst Lone = 1\n",
		}
		if withExtra {
			files["lone/lone_"+other+".go"] = "package lone\n\n// Other is two.\nconst Other = 2\n"
		}
		for name, contents := range files {
			path := filepath.Join(root, filepath.FromSlash(name))
			if directoryError := os.MkdirAll(filepath.Dir(path), 0o755); directoryError != nil {
				test.Fatal(directoryError)
			}
			if writeError := os.WriteFile(path, []byte(contents), 0o644); writeError != nil {
				test.Fatal(writeError)
			}
		}
		request, parseError := parseInvocation([]string{"--contract", fmt.Sprint(contract.Version), "--root", root,
			"--no-cache", "--other-builds"})
		if parseError != nil {
			test.Fatal(parseError)
		}
		registered := rules.All()
		ruleSettings, settingsError := loadSettings(request, registered)
		if settingsError != nil {
			test.Fatal(settingsError)
		}
		described, describeError := describeProject(request)
		if describeError != nil {
			test.Fatal(describeError)
		}
		// What the description started beside its reads ends before the test and its directory do.
		test.Cleanup(described.awaitStarted)
		engine := newPipeline(request, described, ruleSettings, registered, io.Discard, io.Discard, nil)
		isolatePipeline(test, engine)
		// The load alone is asked, so no lint starts beside it.
		engine.overlaps = false
		loaded, loadError := engine.loadWorkspacePackages()
		if loadError != nil {
			test.Fatal(loadError)
		}
		if recorded := loaded.ReadsRecorded > 0; recorded != withExtra {
			test.Errorf("with an extra build %t, the load recorded the reads of %d packages", withExtra,
				loaded.ReadsRecorded)
		}
	}
}

// A host that keeps a system cache and has no extra build records the reads of the standard library's packages alone,
// by the set its session keyed for the table (check.CacheSession.RecordsReadsForTable): the table takes no entry
// without them, and holds no package of the module, whose reads nothing would read.
func TestAHostWithOnlyASystemCacheRecordsTheStandardLibrarysReads(test *testing.T) {
	test.Parallel()

	root := test.TempDir()
	files := map[string]string{
		"go.mod":       "module example.com/records\n\ngo 1.21\n",
		"lone/lone.go": "package lone\n\nimport \"strings\"\n\n// Lone is one.\nvar Lone = strings.Repeat(\"1\", 1)\n",
	}
	for name, contents := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if directoryError := os.MkdirAll(filepath.Dir(path), 0o755); directoryError != nil {
			test.Fatal(directoryError)
		}
		if writeError := os.WriteFile(path, []byte(contents), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
	request, parseError := parseInvocation([]string{"--contract", fmt.Sprint(contract.Version), "--root", root,
		"--no-cache"})
	if parseError != nil {
		test.Fatal(parseError)
	}
	registered := rules.All()
	ruleSettings, settingsError := loadSettings(request, registered)
	if settingsError != nil {
		test.Fatal(settingsError)
	}
	described, describeError := describeProject(request)
	if describeError != nil {
		test.Fatal(describeError)
	}
	// What the description started beside its reads ends before the test and its directory do.
	test.Cleanup(described.awaitStarted)
	engine := newPipeline(request, described, ruleSettings, registered, io.Discard, io.Discard, nil)
	isolatePipeline(test, engine)
	if engine.systemCache.store == nil {
		test.Fatalf("the run keeps no system cache: %v", engine.systemCache.notKept)
	}
	// The session the run makes before its load, as run does.
	engine.session = engine.cacheSession(described.workspace.Environment(), described.goEnvironment,
		cache.FileDigests{}, io.Discard, engine.systemCache)
	// The load alone is asked, so no lint starts beside it.
	engine.overlaps = false
	loaded, loadError := engine.loadWorkspacePackages()
	if loadError != nil {
		test.Fatal(loadError)
	}
	if loaded.ReadsRecorded == 0 {
		test.Fatal("the load recorded no package's reads, want the standard library's")
	}
	for recorded := range loaded.Reads {
		if strings.HasPrefix(recorded.PkgPath, "example.com/") {
			test.Errorf("the load recorded the reads of %s, a package of the module, which no table holds",
				recorded.PkgPath)
		}
	}
	if len(loaded.Packages) != 1 {
		test.Fatalf("want lone alone loaded to analyze, got %d packages", len(loaded.Packages))
	}
	if _, recorded := loaded.Reads[loaded.Packages[0]]; recorded {
		test.Errorf("the load recorded the reads of %s, want them unrecorded", loaded.Packages[0].PkgPath)
	}
}
