package fact_resolver_test

import (
	"bytes"
	"fmt"
	"go/types"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/check"
	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/fact_resolver"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rules/function_purity"
)

// heldBodied is a hand on a run's answers that answers the purity of bottom.Bodied with answer when hands is set, and
// keeps every answer the run computed.
type heldBodied struct {
	hands    bool
	answer   []byte
	lock     sync.Mutex
	computed map[string][]byte
}

func (held *heldBodied) Answer(kindName string, object types.Object) ([]byte, bool) {
	if !held.hands || kindName != function_purity.Kind.Name || object.Name() != "Bodied" {
		return nil, false
	}
	return held.answer, true
}

func (held *heldBodied) Computed(kindName string, object types.Object, encoded []byte) {
	held.lock.Lock()
	defer held.lock.Unlock()
	held.computed[kindName+" "+object.Name()] = encoded
}

// bodiedPurity asks, on middle, the purity of bottom.Bodied, and is the answer it got.
var bodiedPurity = &analysis.Analyzer{
	Name:       "bodied_purity_probe",
	Doc:        "asks the purity of bottom.Bodied and returns it",
	Requires:   []*analysis.Analyzer{function_purity.Analyzer},
	ResultType: reflect.TypeFor[bool](),
	Run: func(pass *analysis.Pass) (any, error) {
		asked, isAsked := pass.ResultOf[function_purity.Analyzer].(*function_purity.Result)
		if !isAsked {
			return nil, fmt.Errorf("function_purity's result is a %T", pass.ResultOf[function_purity.Analyzer])
		}
		return asked.IsPure(bottomFunction(pass.Pkg.Imports()[0], "Bodied")), nil
	},
}

// askBodied runs bodiedPurity on middle, loaded from source, with no store and held's hand on the answers, and is the
// answer middle got and how many functions purity judged.
func askBodied(test *testing.T, held *heldBodied) (bool, int64) {
	test.Helper()
	directory := analysistest.TestData()
	loaded, loadError := load.Packages(directory, []string{"bodiless/middle"}, []string{
		"GOPATH=" + directory, "GO111MODULE=off", "GOWORK=off",
	}, nil)
	if loadError != nil {
		test.Fatalf("loading the fixture: %v", loadError)
	}
	resolver := fact_resolver.New(nil)
	resolver.HoldAnswers(held)
	options := check.DispatchOptions()
	options.Resolver = resolver
	graph, analyzeError := checker.Analyze([]*analysis.Analyzer{bodiedPurity}, loaded.Roots, options)
	if analyzeError != nil {
		test.Fatalf("analyzing the fixture: %v", analyzeError)
	}
	var root *checker.Action
	for _, action := range graph.Roots {
		if action.Err != nil {
			test.Fatalf("asking in %s: %v", action.Package.ID, action.Err)
		}
		root = action
	}
	pure, isAnswer := root.Result.(bool)
	if !isAnswer {
		test.Fatalf("the probe's result is a %T", root.Result)
	}
	return pure, resolver.Counts(function_purity.Kind).Computed.Load()
}

// A hand on the run's answers hears each answer the run computes, bottom.Bodied pure by its body, and an answer it
// hands is the one the reader gets, never computed: handed not pure, middle reads Bodied not pure and purity judges
// nothing. That is what lets the unit-isolation test give a package checked with stubbed bodies the whole package's
// answers (#h350vqr).
func TestAHandOnTheAnswersHearsWhatIsComputedAndAnswersInItsPlace(test *testing.T) {
	test.Parallel()
	listening := &heldBodied{computed: map[string][]byte{}}
	pure, judged := askBodied(test, listening)
	if !pure || judged == 0 {
		test.Fatalf("without a hand answering, want Bodied judged pure, got pure %t after judging %d", pure, judged)
	}
	if heard := string(listening.computed[function_purity.Kind.Name+" Bodied"]); heard != "true" {
		test.Errorf("want the hand to hear Bodied's purity computed as true, heard %q of %v", heard,
			listening.computed)
	}
	answering := &heldBodied{hands: true, answer: []byte("false"), computed: map[string][]byte{}}
	pure, judged = askBodied(test, answering)
	if pure || judged != 0 {
		test.Errorf("handed not pure, want Bodied read not pure and nothing judged, got pure %t after judging %d",
			pure, judged)
	}
	if len(answering.computed) != 0 {
		test.Errorf("handed the one answer asked, want nothing computed, heard %v", answering.computed)
	}
}

// Only a test puts a hand on a run's answers: no file of the module but a test's calls HoldAnswers, so no engine run
// ever answers from anything but its own packages.
func TestOnlyATestHoldsTheAnswers(test *testing.T) {
	test.Parallel()
	root, rootError := filepath.Abs(filepath.Join("..", "..", ".."))
	if rootError != nil {
		test.Fatal(rootError)
	}
	var callers []string
	walkError := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkError error) error {
		if walkError != nil {
			return walkError
		}
		if entry.IsDir() {
			if name := entry.Name(); path != root && (strings.HasPrefix(name, ".") || name == "testdata" ||
				name == "node_modules") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		contents, readError := os.ReadFile(path)
		if readError != nil {
			return readError
		}
		for _, line := range bytes.Split(contents, []byte("\n")) {
			if bytes.Contains(line, []byte("HoldAnswers(")) &&
				!bytes.HasPrefix(line, []byte("func (resolver *Resolver) HoldAnswers(")) &&
				!bytes.HasPrefix(bytes.TrimSpace(line), []byte("//")) {
				callers = append(callers, path)
			}
		}
		return nil
	})
	if walkError != nil {
		test.Fatal(walkError)
	}
	if len(callers) > 0 {
		test.Errorf("want HoldAnswers called only from tests, got calls in %q", callers)
	}
}
