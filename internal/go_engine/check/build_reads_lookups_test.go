package check

import (
	"fmt"
	"slices"
	"testing"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/build_reads"
	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_abbreviated_identifier"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_any"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_uppercase_initialism"
	"github.com/system-inc/cohere/internal/go_engine/rules/deprecated_use"
	"github.com/system-inc/cohere/internal/go_engine/rules/vet/errors_as"
)

// lookupFiles are a module whose package user asks base three things past its own syntax. Its method HTTPSize is
// named by base's interface Sizer and URLText by no interface, which the initialism rule asks the import closure; it
// uses base's deprecated Old and its current New, which SA1019 asks the deprecated facts; and it calls errors.As,
// which vet's errors_as port looks up in package errors' scope. Its method PutMsg takes an any, which no_any and
// abbreviated each ask the closure for a method fixing: base's Store.PutMsg for no_any, an interface method whose
// signature is all no_any's match reads, and none for abbreviated, since page lacks Store's Flush and so implements
// no interface of base's with PutMsg. abbreviated asks of PutMsg alone, the one method name its vocabulary finds
// abbreviated: HTTPSize, URLText and pageError's Error it never asks about (#fbh45hm).
var lookupFiles = map[string]string{
	"go.mod": "module example.com/lookups\n\ngo 1.21\n",
	"base/base.go": "package base\n\n// Sizer is what a sized thing implements.\n" +
		"type Sizer interface{ HTTPSize() int }\n\n// Store keeps values.\n" +
		"type Store interface {\n\tPutMsg(value any)\n\tFlush()\n}\n\n" +
		"// Old is one.\n//\n// Deprecated: use New.\nfunc Old() int { return 1 }\n\n// New is one.\n" +
		"func New() int { return 1 }\n",
	"user/user.go": "package user\n\nimport (\n\t\"errors\"\n\n\t\"example.com/lookups/base\"\n)\n\n" +
		"type page struct{}\n\n// HTTPSize is Sizer's.\n" +
		"func (page) HTTPSize() int { return base.Old() + base.New() }\n\n" +
		"// URLText is no interface's.\nfunc (page) URLText() string { return \"\" }\n\n" +
		"// PutMsg is no interface's.\nfunc (page) PutMsg(value any) { _ = value }\n\n" +
		"type pageError struct{}\n\nfunc (*pageError) Error() string { return \"\" }\n\n" +
		"// Is is whether problem is a page's.\nfunc Is(problem error) bool {\n\tvar target *pageError\n" +
		"\treturn errors.As(problem, &target)\n}\n",
}

// registeredRule is the registry's rule whose analyzer is analyzer, failing the test when none is.
func registeredRule(test *testing.T, analyzer *analysis.Analyzer) rule.Rule {
	test.Helper()

	index := slices.IndexFunc(rules.All(), func(registered rule.Rule) bool { return registered.Analyzer == analyzer })
	if index < 0 {
		test.Fatalf("the registry holds no rule of %s", analyzer.Name)
	}
	return rules.All()[index]
}

// lookupLines are the lookups of query in reads, each as its package, name, subject, answer and text, and for a
// lookup that names its rule, the rule and whether it instantiates.
func lookupLines(reads *build_reads.Reads, query string) []string {
	var lines []string
	for _, lookup := range reads.Lookups() {
		if lookup.Query != query {
			continue
		}
		path, name, subject, answer := "-", "-", "-", "-"
		if lookup.Path != "" {
			path = lookup.Path
		}
		if lookup.Name != "" {
			name = lookup.Name
		}
		if lookup.Subject != nil {
			subject = lookup.Subject.Name()
		}
		if lookup.Answer != nil {
			answer = lookup.Answer.Pkg().Path() + "." + lookup.Answer.Name()
		}
		line := fmt.Sprintf("%s %s %s found=%t %s %q", path, name, subject, lookup.Found, answer, lookup.Text)
		if lookup.Rule != "" {
			line += fmt.Sprintf(" %s instantiates=%t", lookup.Rule, lookup.Instantiates)
		}
		lines = append(lines, line)
	}
	return lines
}

// A rule's questions of other packages past its syntax are recorded with their answers, a miss as not found: the
// initialism rule's question of the import closure's interfaces, SA1019's of the deprecated facts, and errors_as's
// look into package errors' scope. no_any's and abbreviated's Method lookups of one subject are each their own, with
// their own answers, and each names its rule, abbreviated's as instantiating generic candidates (#dscc341).
// abbreviated asks only of a method whose name its vocabulary finds abbreviated, since no other answer can reach a
// finding (#fbh45hm).
func TestARulesLookupsOfOtherPackagesAreRecordedHitAndMiss(test *testing.T) {
	test.Parallel()

	directory := test.TempDir()
	writeFiles(test, directory, lookupFiles)
	loaded := loadRecordingAt(test, directory)
	if len(loaded.Diagnostics) > 0 {
		test.Fatalf("the fixture loaded with diagnostics: %+v", loaded.Diagnostics)
	}
	enabled := []rule.Rule{
		registeredRule(test, consistency_no_uppercase_initialism.Analyzer),
		registeredRule(test, consistency_no_any.Analyzer),
		registeredRule(test, consistency_no_abbreviated_identifier.Analyzer),
		registeredRule(test, deprecated_use.Analyzer),
		registeredRule(test, errors_as.Analyzer),
	}
	result, runError := Run(loaded.Packages, enabled, Options{Reads: loaded.Reads})
	if runError != nil {
		test.Fatal(runError)
	}
	// The type-read walks run past Run, so the reads are whole only once they end.
	result.ReadWalks.Wait()
	if len(result.Crashes) > 0 {
		test.Fatalf("the rules crashed: %+v", result.Crashes)
	}
	var reads *build_reads.Reads
	for _, analyzed := range loaded.Packages {
		if analyzed.PkgPath == "example.com/lookups/user" {
			reads = result.Reads[analyzed]
		}
	}
	if reads == nil {
		test.Fatal("the result holds no reads of user")
	}
	initialism := consistency_no_uppercase_initialism.Name + " instantiates=false"
	noAny := consistency_no_any.Name + " instantiates=false"
	abbreviated := consistency_no_abbreviated_identifier.Name + " instantiates=true"
	expected := map[string][]string{
		build_reads.QueryInterface: {
			`- HTTPSize HTTPSize found=true example.com/lookups/base.Sizer "" ` + initialism,
			`- URLText URLText found=false - "" ` + initialism,
		},
		build_reads.QueryMethod: {
			`- PutMsg PutMsg found=false - "" ` + abbreviated,
			`- PutMsg PutMsg found=true example.com/lookups/base.PutMsg "" ` + noAny,
		},
		build_reads.QueryDeprecated: {
			`errors - - found=false - ""`,
			`errors - As found=false - ""`,
			`example.com/lookups/base - - found=false - ""`,
			`example.com/lookups/base - New found=false - ""`,
			`example.com/lookups/base - Old found=true - "use New. "`,
		},
		build_reads.QueryScope: {
			`errors As - found=true errors.As ""`,
		},
	}
	for query, wanted := range expected {
		if got := lookupLines(reads, query); !slices.Equal(got, wanted) {
			test.Errorf("want user's %s lookups\n%q\ngot\n%q", query, wanted, got)
		}
	}
}
