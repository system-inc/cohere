package rule_runner_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/system-inc/cohere/internal/lint/rule"
	"github.com/system-inc/cohere/internal/lint/rules/adamic"
	"github.com/system-inc/cohere/internal/lint/rules/nexus"
	"github.com/system-inc/cohere/internal/lint/testing"
	"github.com/system-inc/cohere/internal/types/program"
	"github.com/system-inc/cohere/rule_runner"
)

// cycleProject is two modules in one runtime import cycle, each reading the other's binding at load, beside a
// third module that imports one of them without closing a cycle. Every finding the rule can make in it is a
// finding in the cycle's two files; the third proves a file outside any cycle stays quiet.
var cycleProject = map[string]string{
	"tsconfig.json": `{"compilerOptions":{"strict":true,"module":"esnext","target":"esnext","moduleResolution":"bundler"},"include":["*.ts"]}`,
	"Identifiers.ts": "import { RateLimitErrors } from './RateLimitErrors';\n" +
		"export const IdentifierKeys = { Base: 'Base' } as const;\n" +
		"export const Identifiers = { ...RateLimitErrors };\n",
	"RateLimitErrors.ts": "import { IdentifierKeys } from './Identifiers';\n" +
		"export const RateLimitErrors = { RateLimit: IdentifierKeys.Base };\n",
	"Consumer.ts": "import { Identifiers } from './Identifiers';\n" +
		"export const consumed = Identifiers;\n",
}

// buildProject writes files to a directory of its own and builds cohere's program over it.
func buildProject(test *testing.T, files map[string]string) *program.Graph {
	test.Helper()
	directory, resolveSymlinksError := filepath.EvalSymlinks(test.TempDir())
	if resolveSymlinksError != nil {
		test.Fatal(resolveSymlinksError)
	}
	for name, contents := range files {
		if writeError := os.WriteFile(filepath.Join(directory, name), []byte(contents), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
	graph, buildError := program.Build(program.Options{ConfigFileName: "tsconfig.json", CurrentDirectory: directory})
	if buildError != nil {
		test.Fatalf("building: %v", buildError)
	}
	return graph
}

// walkAndRun is subject's findings on files twice, through cohere's own walk and through RunRule, on one program,
// each sorted by place. The walk is the reference because it is what every cohere run reports through.
func walkAndRun(
	test *testing.T,
	files map[string]string,
	subject rule.Rule[rule.NoOptions],
) (walked []rule_runner.Finding, ran []rule_runner.Finding) {
	test.Helper()
	graph := buildProject(test, files)
	result, walkError := graph.Walk(context.Background(), graph.ProjectFiles(), rule_testing.EntriesOf(test, subject))
	if walkError != nil {
		test.Fatalf("walking: %v", walkError)
	}
	for _, diagnostic := range result.Diagnostics {
		fileName, line, column := diagnostic.Location()
		walked = append(walked, rule_runner.Finding{
			Rule:    diagnostic.RuleName,
			File:    fileName,
			Line:    line,
			Column:  column,
			Message: diagnostic.Message.Description,
		})
	}
	typeChecker, done := graph.Program.GetTypeChecker(context.Background())
	ran, runRuleError := rule_runner.RunRule(graph.Program, typeChecker, graph.ProjectFiles(), subject.Name)
	done()
	if runRuleError != nil {
		test.Fatalf("running: %v", runRuleError)
	}
	byPlace := func(left rule_runner.Finding, right rule_runner.Finding) int {
		if left.File != right.File {
			if left.File < right.File {
				return -1
			}
			return 1
		}
		return left.Line*10000 + left.Column - right.Line*10000 - right.Column
	}
	slices.SortFunc(walked, byPlace)
	slices.SortFunc(ran, byPlace)
	return walked, ran
}

// What RunRule returns is what cohere's own walk reports, finding for finding, on the same program: the rule,
// the file, the line, the column and the message, each compared whole.
func TestRunRuleFindsWhatTheWalkFinds(test *testing.T) {
	test.Parallel()
	want, got := walkAndRun(test, cycleProject, nexus.CorrectnessNoImportCycleLoadTimeRead)

	// The fixture is built to fire, so an empty pair can't pass as agreement: both cycle files read the other's
	// binding at load, and the consumer, in no cycle, reads nothing early.
	if len(want) != 2 {
		test.Fatalf("the walk found %d findings, want 2 (one in each file of the cycle): %+v", len(want), want)
	}
	if !slices.Equal(got, want) {
		test.Errorf("RunRule found\n%+v\nthe walk found\n%+v", got, want)
	}
	for _, finding := range got {
		if filepath.Base(finding.File) == "Consumer.ts" {
			test.Errorf("a file in no cycle was reported: %+v", finding)
		}
	}
}

// The relation rules Adamic runs in place of its own copies (#xjdce2d step 4) find through RunRule what the walk
// finds, each on a program built to fire once and a sound neighbour that must stay quiet.
func TestRunRuleFindsWhatTheWalkFindsForTheRelationRules(test *testing.T) {
	test.Parallel()
	const configuration = `{"compilerOptions":{"strict":true,"exactOptionalPropertyTypes":true,"module":"esnext","target":"esnext","moduleResolution":"bundler"},"include":["*.ts"]}`
	for _, testCase := range []struct {
		subject rule.Rule[rule.NoOptions]
		source  string
	}{
		{
			// A Dog[] seen as an Animal[] lets a Cat be pushed into the dogs. The readonly view beside it is sound.
			subject: adamic.InvariantMutable,
			source: "interface Animal { name: string }\n" +
				"interface Dog extends Animal { bark(): void }\n" +
				"export function pen(dogs: Dog[]): void {\n" +
				"    const animals: Animal[] = dogs;\n" +
				"    const view: readonly Animal[] = dogs;\n" +
				"    void animals; void view;\n" +
				"}\n",
		},
		{
			// A DogBox is a Box<Dog>, so seen as a Box<Animal> it takes a Cat. A Box<Dog> seen as itself is sound.
			subject: adamic.NominalClass,
			source: "class Animal { name = '' }\n" +
				"class Dog extends Animal { bark(): void {} }\n" +
				"class Box<T> { constructor(public item: T) {} }\n" +
				"class DogBox extends Box<Dog> {}\n" +
				"export function kennel(box: DogBox, same: Box<Dog>): void {\n" +
				"    const wide: Box<Animal> = box;\n" +
				"    const kept: Box<Dog> = same;\n" +
				"    void wide; void kept;\n" +
				"}\n",
		},
	} {
		test.Run(testCase.subject.Name, func(test *testing.T) {
			test.Parallel()
			want, got := walkAndRun(
				test,
				map[string]string{"tsconfig.json": configuration, "Main.ts": testCase.source},
				testCase.subject,
			)
			if len(want) != 1 {
				test.Fatalf(
					"the walk found %d findings, want exactly 1 (the hole, not its sound neighbour): %+v",
					len(want),
					want,
				)
			}
			if !slices.Equal(got, want) {
				test.Errorf("RunRule found\n%+v\nthe walk found\n%+v", got, want)
			}
		})
	}
}

// A rule off the allowlist is refused by name, whether cohere has it or not, before anything runs.
func TestRunRuleRefusesARuleOffTheAllowlist(test *testing.T) {
	test.Parallel()
	graph := buildProject(test, cycleProject)
	typeChecker, done := graph.Program.GetTypeChecker(context.Background())
	defer done()

	// Allowed first: a refusal that refused everything would pass both cases below.
	if _, runRuleError := rule_runner.RunRule(
		graph.Program,
		typeChecker,
		graph.ProjectFiles(),
		nexus.CorrectnessNoImportCycleLoadTimeRead.Name,
	); runRuleError != nil {
		test.Fatalf("the allowed rule was refused: %v", runRuleError)
	}
	for _, name := range []string{"no-console", "nexus/correctness-no-such-rule"} {
		findings, runRuleError := rule_runner.RunRule(graph.Program, typeChecker, graph.ProjectFiles(), name)
		var notAllowed rule_runner.NotAllowedError
		if !errors.As(runRuleError, &notAllowed) || notAllowed.Rule != name {
			test.Errorf(
				"%s: got findings %v and error %v, want NotAllowedError naming it",
				name,
				findings,
				runRuleError,
			)
		}
	}
}
