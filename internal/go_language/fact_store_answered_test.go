package go_language

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/go_language/contract"
	"github.com/system-inc/cohere/internal/go_language/rules"
)

// A package the store answered a question about leaves the host no side of it, as an unsure one does, and the build
// lints it whole (#z9fze0t): an answer the store gave was judged in another run, and nothing of what it rests on was
// judged in this one, so a function with no record there is no longer one no answer rests on, which every kind that
// records every judgment counts on (fact_resolver.Kind.RecordsEveryJudgment). In the fact-store-answered fixture a
// first run writes p's entry with only the answers its questions computed, Mid's purity among them and not inner's, on
// which it rests. Then q discards p.Other's result, which the entry does not answer, so the host's run of the edited
// tree loads p again from source, where User's question of Mid's purity is answered from the entry and inner is never
// judged. p gets no side, counted on the census's sides line as answered from the store, and q, whose side holds
// nothing of the store, keeps its own.
func TestAPackageTheStoreAnsweredAQuestionAboutHasNoSideForTheFactsReach(test *testing.T) {
	test.Parallel()

	root := copyFixture(test, filepath.Join("testdata", "fact-store-answered"))
	systemCache := test.TempDir()
	run := func(configure func(*pipeline)) *pipeline {
		test.Helper()
		request, parseError := parseInvocation([]string{"--contract", fmt.Sprint(contract.Version), "--root", root,
			"--no-fix", "--build-dependence"})
		if parseError != nil {
			test.Fatal(parseError)
		}
		request.standardLibrary.systemCacheRoot = systemCache
		request.answersInPart = map[string]bool{"example.com/storeanswered/p": true}
		engine, standardOutput, standardError, exitCode := runOverRequest(test, request, rules.All(), configure)
		if exitCode != exitClean && exitCode != exitFindings {
			test.Fatalf("the run exited %d:\n%s\n%s", exitCode, standardOutput, standardError)
		}
		return engine
	}
	run(nil)
	upper := filepath.Join(root, "q", "q.go")
	contents, readError := os.ReadFile(upper)
	if readError != nil {
		test.Fatal(readError)
	}
	added := "\n// Drop discards p.Other's result.\nfunc Drop() {\n\tp.Other(2)\n}\n"
	if writeError := os.WriteFile(upper, append(contents, added...), 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	censusFile := filepath.Join(test.TempDir(), "reach.txt")
	edited := run(func(engine *pipeline) { engine.bodyReachCensus = censusFile })

	if edited.factReachSides == nil {
		test.Fatal("the edited run under the census made no sides for the facts' reach")
	}
	if side := edited.factReachSides.packages["example.com/storeanswered/p"]; side != nil {
		var asked []string
		for _, asking := range side.askings {
			asked = append(asked, fmt.Sprintf("%s %s %s", asking.Kind, asking.Function.Path, asking.Encoded))
		}
		slices.Sort(asked)
		test.Errorf("p has a side, asking %q, with the sides %d, uncaptured %d, want none: Mid's purity came from the "+
			"store", asked, len(edited.factReachSides.packages), edited.factReachSides.uncaptured)
	}
	if edited.factReachSides.packages["example.com/storeanswered/q"] == nil {
		test.Error("q has no side, want one: the store answered nothing about its objects")
	}
	written, readError := os.ReadFile(censusFile)
	if readError != nil {
		test.Fatal(readError)
	}
	line := "fact reach: packages given a side 1; none, uncaptured 0, answered from the store 1, unrecorded 0, " +
		"order unknown 0"
	if !slices.Contains(strings.Split(string(written), "\n"), line) {
		test.Errorf("the census file has no line %q:\n%s", line, written)
	}
}
