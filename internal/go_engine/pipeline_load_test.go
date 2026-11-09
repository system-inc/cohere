package go_engine

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/contract"
	"github.com/system-inc/cohere/internal/go_engine/rules"
)

// loadPackagesPath is package load's import path, whose loads only loadPackages may call.
const loadPackagesPath = "github.com/system-inc/cohere/internal/go_engine/load"

// Every load of the workspace goes through loadPackages, which returns the limit to the cap before it and arms the
// raise around it, so a load added anywhere else in the engine would run under whatever limit the last load's raise
// left. Read from the package's source, not its tests.
func TestOnlyLoadPackagesLoads(test *testing.T) {
	test.Parallel()
	files, globError := filepath.Glob("*.go")
	if globError != nil {
		test.Fatalf("listing the package's files: %v", globError)
	}
	fileSet := token.NewFileSet()
	calls := 0
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		parsed, parseError := parser.ParseFile(fileSet, file, nil, parser.SkipObjectResolution)
		if parseError != nil {
			test.Fatalf("parsing %s: %v", file, parseError)
		}
		name := importedAs(parsed, loadPackagesPath)
		if name == "" {
			continue
		}
		for _, declaration := range parsed.Decls {
			function, isFunction := declaration.(*ast.FuncDecl)
			allowed := isFunction && function.Name.Name == "loadPackages" && function.Recv != nil
			ast.Inspect(declaration, func(node ast.Node) bool {
				call, isCall := node.(*ast.CallExpr)
				if !isCall {
					return true
				}
				selector, isSelector := call.Fun.(*ast.SelectorExpr)
				if !isSelector || !strings.HasPrefix(selector.Sel.Name, "Packages") {
					return true
				}
				if identifier, isIdentifier := selector.X.(*ast.Ident); !isIdentifier || identifier.Name != name {
					return true
				}
				calls++
				if !allowed {
					test.Errorf("%s calls load.%s outside loadPackages, so that load skips the memory limit's reset "+
						"and its late raise; load through engine.loadPackages", fileSet.Position(call.Pos()),
						selector.Sel.Name)
				}
				return true
			})
		}
	}
	if calls == 0 {
		test.Error("found no call of package load's loads at all, so this test reads nothing")
	}
}

// importedAs is the name file refers to path by, empty when it does not import it.
func importedAs(file *ast.File, path string) string {
	for _, imported := range file.Imports {
		unquoted, unquoteError := strconv.Unquote(imported.Path.Value)
		if unquoteError != nil || unquoted != path {
			continue
		}
		if imported.Name != nil {
			return imported.Name.Name
		}
		return filepath.Base(path)
	}
	return ""
}

// memoryWatch is a fake memory limit and collector reader for a run in process: it keeps the limit set, and at each
// load's end, when the raise reads the collector, the limit the load ran to its end under.
type memoryWatch struct {
	lock    sync.Mutex
	current int64
	// raised is whether a raise has lifted the limit past the cap, and endedUnder the limit at each load's end.
	raised     bool
	endedUnder []int64
}

func (watch *memoryWatch) setMemoryLimit(limit int64) int64 {
	watch.lock.Lock()
	defer watch.lock.Unlock()
	previous := watch.current
	watch.current = limit
	if limit > engineMemoryCap {
		watch.raised = true
	}
	return previous
}

// collecting is read at each load's end, and says no collection is in flight, so each raise lands at once.
func (watch *memoryWatch) collecting() bool {
	watch.lock.Lock()
	defer watch.lock.Unlock()
	watch.endedUnder = append(watch.endedUnder, watch.current)
	return false
}

// A run whose long-line wrap writes loads again, and the reload runs under the cap: the first load's raise lifted the
// limit, and the reload set it back before it started, so it ended under the cap as the first load did.
func TestAReloadAfterTheRaiseRunsUnderTheCap(test *testing.T) {
	test.Parallel()
	root := copyFixture(test, filepath.Join("testdata", "long-lines"))
	file := filepath.Join(root, "long_lines.go")
	original, readError := os.ReadFile(file)
	if readError != nil {
		test.Fatalf("reading %s: %v", file, readError)
	}
	watch := &memoryWatch{current: engineMemoryCap}
	memory := &engineMemory{
		record: contract.Memory{
			LimitBytes: engineMemoryCap, CapBytes: engineMemoryCap, Source: contract.MemorySourceCap,
		},
		inheritedBytes: 32 << 30,
		setMemoryLimit: watch.setMemoryLimit,
		collecting:     watch.collecting,
	}
	var standardOutput bytes.Buffer
	var standardError bytes.Buffer

	exitCode := run([]string{"--contract", fmt.Sprint(contract.Version), "--root", root, "--no-cache"},
		&standardOutput, &standardError, rules.All(), memory)

	if exitCode != exitFindings && exitCode != exitClean {
		test.Fatalf("exit %d; stderr:\n%s", exitCode, standardError.String())
	}
	wrapped, readError := os.ReadFile(file)
	if readError != nil || bytes.Equal(wrapped, original) {
		test.Fatalf("the run left %s as it was (%v), so it never loaded again", file, readError)
	}
	watch.lock.Lock()
	defer watch.lock.Unlock()
	if len(watch.endedUnder) < 2 || !watch.raised {
		test.Fatalf("the run ended %d loads and raised %t, want a reload after a raise", len(watch.endedUnder),
			watch.raised)
	}
	for index, limit := range watch.endedUnder {
		if limit != engineMemoryCap {
			test.Errorf("load %d of %d ended under a limit of %d, want the cap, %d", index+1, len(watch.endedUnder),
				limit, engineMemoryCap)
		}
	}
}

// Not parallel: it turns the collector's automatic collections off for the whole process and counts collections, so
// only its own runtime.GC call may complete one, which a test running beside it would not leave alone.
func TestALastLoadsRaiseLiftsNothingDuringTheNext(test *testing.T) {
	previousPercent := debug.SetGCPercent(-1)
	test.Cleanup(func() { debug.SetGCPercent(previousPercent) })
	watch := &memoryWatch{current: engineMemoryCap}
	engine := &pipeline{memory: &engineMemory{
		record: contract.Memory{
			LimitBytes: engineMemoryCap, CapBytes: engineMemoryCap, Source: contract.MemorySourceCap,
		},
		inheritedBytes: 32 << 30,
		setMemoryLimit: watch.setMemoryLimit,
		// Every load ends with a collection in flight, so each raise waits on its sentinel's finalizer.
		collecting: func() bool { return true },
	}}
	engine.beginLoad()
	first := engine.lateRaise
	if first == nil {
		test.Fatal("the first load armed no raise")
	}
	first.endLoad()

	engine.beginLoad()
	runtime.GC()
	waitFor(test, "the first load's sentinel", func() bool { return first.collections.Load() >= 1 })

	watch.lock.Lock()
	defer watch.lock.Unlock()
	if watch.current != engineMemoryCap || watch.raised {
		test.Errorf("the first load's collection completing during the next load left the limit at %d (raised %t), "+
			"want the cap, %d", watch.current, watch.raised, engineMemoryCap)
	}
}
