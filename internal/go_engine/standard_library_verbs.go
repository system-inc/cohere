package go_engine

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"go/parser"
	"go/token"
	"go/version"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"

	"github.com/system-inc/cohere/internal/go_engine/cache"
	"github.com/system-inc/cohere/internal/go_engine/check"
	"github.com/system-inc/cohere/internal/go_engine/contract"
	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/standard_library_table"
)

// The release's hidden verbs for the standard library's table, each the first argument after the go-engine verb.
const (
	// standardLibraryTableVerb generates the table a release embeds for one target.
	standardLibraryTableVerb = "standard-library-table"

	// verifyStandardLibraryVerb is the release's check of the table the binary embeds, on the target it was built
	// for.
	verifyStandardLibraryVerb = "verify-std-facts"
)

// binaryVerb is the front door's go-engine verb (command/cohere's goEngineVerb), which a generator's child of this
// binary is started with before its own verb, as the front door starts the engine.
const binaryVerb = "go-engine"

// standardLibraryVerbs are the verbs runStandardLibraryVerb runs, the children's among them.
var standardLibraryVerbs = []string{standardLibraryTableVerb, verifyStandardLibraryVerb, standardLibraryVariantVerb,
	standardLibraryCanonicalVerb}

// isStandardLibraryVerb reports whether a command line names one of standardLibraryVerbs first, which no command line
// of the front door's does: it starts with --contract, and every path it sends is absolute.
func isStandardLibraryVerb(arguments []string) bool {
	return len(arguments) > 0 && slices.Contains(standardLibraryVerbs, arguments[0])
}

// binaryGenerator is the generator of this binary: its children are this executable under the go-engine verb, and
// its stamp the one it was linked with.
func binaryGenerator() (standardLibraryGenerator, error) {
	executable, executableError := os.Executable()
	if executableError != nil {
		return standardLibraryGenerator{}, fmt.Errorf("finding this executable, whose children compute the "+
			"standard library: %w", executableError)
	}
	return standardLibraryGenerator{
		executable: executable,
		prefix:     []string{binaryVerb},
		stamp:      standard_library_table.ReleaseStamp,
	}, nil
}

// runStandardLibraryVerb runs a command line that isStandardLibraryVerb accepts, with registered as the rules, table as
// the table the binary embeds, and generator starting the children, and returns the exit code: 0 for done, 1 for a
// failure, which names what failed on standardError, and 2 for a command line refused before anything ran.
func runStandardLibraryVerb(
	arguments []string,
	standardOutput io.Writer,
	standardError io.Writer,
	registered []rule.Rule,
	table *cache.StandardLibraryTable,
	generator standardLibraryGenerator,
) int {
	switch arguments[0] {
	case standardLibraryTableVerb:
		return generateStandardLibraryTable(arguments[1:], standardOutput, standardError, generator)
	case verifyStandardLibraryVerb:
		return verifyStandardLibrary(arguments[1:], standardOutput, standardError, table, generator)
	case standardLibraryVariantVerb:
		return runStandardLibraryVariant(arguments[1:], standardError, registered, generator.stamp)
	case standardLibraryCanonicalVerb:
		return runStandardLibraryCanonical(arguments[1:], standardError, registered)
	}
	return refuse(standardError, fmt.Errorf("%s is no verb of the standard library's table", arguments[0]))
}

// generateStandardLibraryTable is `cohere go-engine standard-library-table --out <file> --target <goos>/<goarch>
// --versions <go1.x.y,...> --cgo <1,0>`: every version under every `CGO_ENABLED` value, each in a child (see
// standardLibraryGenerator), then one canonical table of them all written to the file. It prints a line for each
// variant, with its package count and the size of a table of it alone, and one for the table. Any variant that fails
// fails the whole, and nothing is written.
func generateStandardLibraryTable(
	arguments []string,
	standardOutput io.Writer,
	standardError io.Writer,
	generator standardLibraryGenerator,
) int {
	flags := flag.NewFlagSet(standardLibraryTableVerb, flag.ContinueOnError)
	flags.SetOutput(standardError)
	output := flags.String("out", "", "the file the table is written to")
	target := flags.String("target", "", "the target, as GOOS/GOARCH")
	versions := flags.String("versions", "", "the Go versions, comma-separated, each as GOTOOLCHAIN names it")
	cgoValues := flags.String("cgo", "", "the CGO_ENABLED values, comma-separated")
	if parseError := flags.Parse(arguments); parseError != nil {
		return exitRefused
	}
	requests, requestsError := standardLibraryRequests(flags.Args(), *output, *target, *versions, *cgoValues)
	if requestsError != nil {
		return refuse(standardError, requestsError)
	}
	if generator.stamp == "" {
		return refuse(standardError, errors.New("refusing to generate the standard library's table: this binary "+
			"carries no release stamp, and every key holds it"))
	}
	tables := make([][]byte, 0, len(requests))
	variants := make([]cache.StandardLibraryVariant, 0, len(requests))
	for _, request := range requests {
		table, variant, variantError := generator.variantTable(request)
		if variantError != nil {
			return fail(standardError, variantError)
		}
		tables = append(tables, table)
		variants = append(variants, variant)
	}
	canonical, canonicalError := generator.canonicalTable(tables)
	if canonicalError != nil {
		return fail(standardError, canonicalError)
	}
	written := cache.NewStandardLibraryTable(canonical)
	for index, variant := range variants {
		alone := map[cache.Key]cache.StandardLibraryEntry{}
		for _, key := range cache.NewStandardLibraryTable(tables[index]).Keys() {
			entry, held := written.Lookup(key)
			if !held {
				return fail(standardError, fmt.Errorf("writing the standard library's table: it lost an entry of %s",
					variantLabel(requests[index])))
			}
			alone[key] = entry
		}
		// Ignored: the line is the report, and a stdout that cannot be written to fails the run below.
		_, _ = fmt.Fprintf(standardOutput, "%s %s cgo %s: %d packages, %d bytes alone\n", variant.GoVersion,
			variant.Target, variant.CgoEnabled, variant.Packages,
			len(cache.WriteStandardLibraryTable(alone, []cache.StandardLibraryVariant{variant})))
	}
	if writeError := os.WriteFile(*output, canonical, 0o644); writeError != nil {
		return fail(standardError, fmt.Errorf("writing the standard library's table: %w", writeError))
	}
	if _, printError := fmt.Fprintf(standardOutput, "table: %d variants, %d keys, %d bytes deduplicated and gzip, "+
		"at %s\n", len(variants), len(written.Keys()), len(canonical), *output); printError != nil {
		return fail(standardError, printError)
	}
	return exitClean
}

// standardLibraryRequests are the variants a generator's command line asks for, every version under every
// `CGO_ENABLED` value in the order given, or why the command line is refused: a positional, a flag missing, a target
// that is not `GOOS/GOARCH`, a version that is no Go release, a `CGO_ENABLED` that is neither 1 nor 0, or a variant
// named twice.
func standardLibraryRequests(
	positionals []string,
	output string,
	target string,
	versions string,
	cgoValues string,
) ([]standardLibraryVariantRequest, error) {
	if len(positionals) > 0 {
		return nil, fmt.Errorf("%s takes no positional arguments, and was given %q", standardLibraryTableVerb,
			positionals)
	}
	for name, value := range map[string]string{"out": output, "target": target, "versions": versions,
		"cgo": cgoValues} {
		if value == "" {
			return nil, fmt.Errorf("%s needs --%s", standardLibraryTableVerb, name)
		}
	}
	if goos, goarch, cut := strings.Cut(target, "/"); !cut || goos == "" || goarch == "" || strings.Contains(goarch,
		"/") {
		return nil, fmt.Errorf("--target %s is not GOOS/GOARCH", target)
	}
	var requests []standardLibraryVariantRequest
	for goVersion := range strings.SplitSeq(versions, ",") {
		if !version.IsValid(goVersion) {
			return nil, fmt.Errorf("--versions names %q, which is no Go version", goVersion)
		}
		for cgoEnabled := range strings.SplitSeq(cgoValues, ",") {
			if cgoEnabled != "1" && cgoEnabled != "0" {
				return nil, fmt.Errorf("--cgo names %q, and CGO_ENABLED is 1 or 0", cgoEnabled)
			}
			request := standardLibraryVariantRequest{goVersion: goVersion, target: target, cgoEnabled: cgoEnabled}
			if slices.Contains(requests, request) {
				return nil, fmt.Errorf("the command line names %s twice", variantLabel(request))
			}
			requests = append(requests, request)
		}
	}
	return requests, nil
}

// verifyStandardLibrary is `cohere go-engine verify-std-facts`, the release's check of table, the one the binary
// embeds: for every variant it holds for this binary's own target, the generator recomputes that variant's standard
// library with no table read, and each package's export digest, output digest and persisted reads must equal, by value,
// those of the table's entry under the same key. It fails on any difference, on a key it computed that the table does
// not hold, on a variant whose package count is not the table's, on a variant it could not recompute, on any entry of
// the table no variant recomputed, and on a table that holds no variant for this target. It prints a line for each
// variant with the count it checked, so a run that checked nothing cannot pass.
func verifyStandardLibrary(
	arguments []string,
	standardOutput io.Writer,
	standardError io.Writer,
	table *cache.StandardLibraryTable,
	generator standardLibraryGenerator,
) int {
	if len(arguments) > 0 {
		return refuse(standardError, fmt.Errorf("%s takes no arguments, and was given %q", verifyStandardLibraryVerb,
			arguments))
	}
	if generator.stamp == "" {
		return refuse(standardError, errors.New("refusing to check the standard library's table: this binary "+
			"carries no release stamp, so it reads no table"))
	}
	if table == nil || table.Empty() {
		return fail(standardError, errors.New("checking the standard library's table: this binary carries none"))
	}
	if problem := table.Problem(); problem != nil {
		return fail(standardError, fmt.Errorf("checking the standard library's table: %w", problem))
	}
	target := runtime.GOOS + "/" + runtime.GOARCH
	var variants []cache.StandardLibraryVariant
	for _, variant := range table.Variants() {
		if variant.Target == target {
			variants = append(variants, variant)
		}
	}
	if len(variants) == 0 {
		return fail(standardError, fmt.Errorf("checking the standard library's table: it holds no variant for %s, "+
			"this binary's target, of the %d it holds", target, len(table.Variants())))
	}
	failed := false
	recomputed := map[cache.Key]bool{}
	for _, variant := range variants {
		request := standardLibraryVariantRequest{goVersion: variant.GoVersion, target: variant.Target,
			cgoEnabled: variant.CgoEnabled}
		recomputedTable, _, variantError := generator.variantTable(request)
		if variantError != nil {
			reportLine(standardError, fmt.Sprintf("%s: not recomputed: %v", variantLabel(request), variantError))
			failed = true
			continue
		}
		checked, differing := 0, 0
		fresh := cache.NewStandardLibraryTable(recomputedTable)
		for _, key := range fresh.Keys() {
			recomputed[key] = true
			// Ignored: the key is one of the table's own.
			computed, _ := fresh.Lookup(key)
			difference := differenceFrom(table, key, computed)
			if difference != "" {
				reportLine(standardError, variantLabel(request)+": "+difference)
				differing++
				continue
			}
			checked++
		}
		if differing > 0 || checked != variant.Packages {
			failed = true
		}
		reportLine(standardOutput, fmt.Sprintf("%s: %d checked, %d differ, of the %d the table says it holds",
			variantLabel(request), checked, differing, variant.Packages))
	}
	unchecked := 0
	for _, key := range table.Keys() {
		if !recomputed[key] {
			unchecked++
		}
	}
	if unchecked > 0 {
		reportLine(standardError, fmt.Sprintf("%d entries of the table no variant recomputed", unchecked))
		failed = true
	}
	if failed {
		return fail(standardError, errors.New("the standard library's table is not what this binary computes"))
	}
	reportLine(standardOutput, fmt.Sprintf("the standard library's table: every one of its %d entries recomputed "+
		"alike", len(table.Keys())))
	return exitClean
}

// differenceFrom is how the entry table holds under key differs from computed, the entry a check recomputed under the
// same key, by package, export digest, output digest and persisted reads, or empty when it does not.
func differenceFrom(table *cache.StandardLibraryTable, key cache.Key, computed cache.StandardLibraryEntry) string {
	computedDigests, computedError := check.StandardLibraryDigestsOf(computed)
	if computedError != nil {
		return fmt.Sprintf("the recomputed entry under %s does not read: %v", key, computedError)
	}
	shipped, held := table.Lookup(key)
	if !held {
		return computedDigests.PackagePath + ": the table holds no entry under its key"
	}
	shippedDigests, shippedError := check.StandardLibraryDigestsOf(shipped)
	switch {
	case shippedError != nil:
		return fmt.Sprintf("%s: the table's entry does not read: %v", computedDigests.PackagePath, shippedError)
	case shippedDigests.PackagePath != computedDigests.PackagePath:
		return fmt.Sprintf("%s: the table holds %s under its key", computedDigests.PackagePath,
			shippedDigests.PackagePath)
	case shippedDigests.Export != computedDigests.Export:
		return computedDigests.PackagePath + ": its export digest differs"
	case !bytes.Equal(shippedDigests.Output, computedDigests.Output):
		return computedDigests.PackagePath + ": its output digest differs"
	case shippedDigests.Reads != computedDigests.Reads:
		return computedDigests.PackagePath + ": its persisted reads differ"
	}
	return ""
}

// runStandardLibraryVariant is the child that computes one variant: `standard-library-variant --root <probe> --out
// <file>`, under the variant's environment, which its parent set. It runs the engine over the probe, cached in an
// empty store of the probe's own, collecting every standard-library package under stamp, and writes their table with
// the variant go env reports. Every package the probe imports must be among them.
func runStandardLibraryVariant(arguments []string, standardError io.Writer, registered []rule.Rule, stamp string) int {
	flags := flag.NewFlagSet(standardLibraryVariantVerb, flag.ContinueOnError)
	flags.SetOutput(standardError)
	root := flags.String("root", "", "the probe module")
	output := flags.String("out", "", "the file the variant's table is written to")
	if parseError := flags.Parse(arguments); parseError != nil {
		return exitRefused
	}
	if *root == "" || *output == "" || flags.NArg() > 0 {
		return refuse(standardError, fmt.Errorf("%s takes --root and --out, and nothing else",
			standardLibraryVariantVerb))
	}
	if stamp == "" {
		return refuse(standardError, errors.New("refusing to compute the standard library: this binary carries no "+
			"release stamp, and every key holds it"))
	}
	collected := map[cache.Key]check.StandardLibraryRunEntry{}
	var engineError bytes.Buffer
	exitCode := runWithStandardLibrary([]string{"--contract", strconv.Itoa(contract.Version), "--root", *root,
		"--no-fix"}, io.Discard, &engineError, registered, nil,
		builtInStandardLibrary{stamp: stamp, collected: collected})
	if (exitCode != exitClean && exitCode != exitFindings) || engineError.Len() > 0 {
		return fail(standardError, fmt.Errorf("computing the standard library: the engine exited %d: %s", exitCode,
			strings.TrimSpace(engineError.String())))
	}
	values, valuesError := readGoEnvironment(*root, nil)
	if valuesError != nil {
		return fail(standardError, valuesError)
	}
	imported, importsError := probeImports(*root)
	if importsError != nil {
		return fail(standardError, importsError)
	}
	collectedPaths := map[string]bool{}
	for _, entry := range collected {
		collectedPaths[entry.PackagePath] = true
	}
	var missing []string
	for _, importPath := range imported {
		if !collectedPaths[importPath] {
			missing = append(missing, importPath)
		}
	}
	if len(missing) > 0 {
		return fail(standardError, fmt.Errorf("computing the standard library: the run collected no entry of %s",
			strings.Join(missing, ", ")))
	}
	variant := cache.StandardLibraryVariant{
		Target:     values["GOOS"] + "/" + values["GOARCH"],
		CgoEnabled: values["CGO_ENABLED"],
		GoVersion:  values["GOVERSION"],
		Packages:   len(collected),
	}
	table, tableError := check.StandardLibraryTableOf(collected, values["GOROOT"],
		[]cache.StandardLibraryVariant{variant})
	if tableError != nil {
		return fail(standardError, tableError)
	}
	if writeError := os.WriteFile(*output, table, 0o644); writeError != nil {
		return fail(standardError, fmt.Errorf("writing the variant's table: %w", writeError))
	}
	return exitClean
}

// probeImports are the import paths the probe's one file names, in its order.
func probeImports(root string) ([]string, error) {
	parsed, parseError := parser.ParseFile(token.NewFileSet(), filepath.Join(root, probeFileName), nil,
		parser.ImportsOnly)
	if parseError != nil {
		return nil, fmt.Errorf("reading the probe's imports: %w", parseError)
	}
	imported := make([]string, 0, len(parsed.Imports))
	for _, importSpec := range parsed.Imports {
		importPath, unquoteError := strconv.Unquote(importSpec.Path.Value)
		if unquoteError != nil {
			return nil, fmt.Errorf("reading the probe's imports: %w", unquoteError)
		}
		imported = append(imported, importPath)
	}
	return imported, nil
}

// runStandardLibraryCanonical is the child that writes the canonical table: `standard-library-canonical --out <file>
// <table>...`, each table a variant's (see check.CanonicalStandardLibraryTable). It encodes nothing with gob before,
// which the canonical table's bytes depend on.
func runStandardLibraryCanonical(arguments []string, standardError io.Writer, registered []rule.Rule) int {
	flags := flag.NewFlagSet(standardLibraryCanonicalVerb, flag.ContinueOnError)
	flags.SetOutput(standardError)
	output := flags.String("out", "", "the file the canonical table is written to")
	if parseError := flags.Parse(arguments); parseError != nil {
		return exitRefused
	}
	if *output == "" || flags.NArg() == 0 {
		return refuse(standardError, fmt.Errorf("%s takes --out and one table or more", standardLibraryCanonicalVerb))
	}
	tables := make([][]byte, 0, flags.NArg())
	for _, input := range flags.Args() {
		table, readError := os.ReadFile(input)
		if readError != nil {
			return fail(standardError, fmt.Errorf("reading a variant's table: %w", readError))
		}
		tables = append(tables, table)
	}
	canonical, canonicalError := check.CanonicalStandardLibraryTable(tables, registered)
	if canonicalError != nil {
		return fail(standardError, canonicalError)
	}
	if writeError := os.WriteFile(*output, canonical, 0o644); writeError != nil {
		return fail(standardError, fmt.Errorf("writing the canonical table: %w", writeError))
	}
	return exitClean
}

// fail writes why a verb failed as one sentence and returns 1.
func fail(standardError io.Writer, failure error) int {
	// Ignored: stderr is the only place the reason can go, and exit 1 says the verb failed either way.
	_, _ = fmt.Fprintln(standardError, refusalSentence(failure))
	return exitFindings
}

// reportLine writes one line of a verb's report.
func reportLine(writer io.Writer, line string) {
	// Ignored: the exit code is the verdict, and a line that cannot be written changes none.
	_, _ = fmt.Fprintln(writer, line)
}
