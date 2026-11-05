package guard

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// guardOverlay is the overlay the guards here read the rule packages through: the one GOFLAGS names, as
// file path to contents, or nil when GOFLAGS names none.
//
// The guards load the rule packages with internal/gotypes, which runs `go list` and then parses every
// file itself, from disk. `go test -overlay x.json` reaches neither: the flag builds the test binary and
// stops there, and nothing in the binary can see it (measured: no build setting records it, and the
// environment is untouched). So a guard run that way checked the working tree while the command line
// said it checked the overlay, and two wrong "dispatch green" claims went through on it. GOFLAGS is
// inherited by the test binary, so `GOFLAGS=-overlay=x.json` is the spelling it can read; this hands its
// contents to the loader's own parser, which keeps the flag out of the go commands it runs, since go list
// only answers for the files the loader reads anyway and a runner of prebuilt tests refuses it.
//
// The spelling that cannot work is refused rather than ignored, when it can be seen: on Unix the `go`
// command that started this binary is asked for its arguments, and an -overlay there that GOFLAGS does
// not carry fails the guard by name.
func guardOverlay(test *testing.T) map[string][]byte {
	test.Helper()
	return guardOverlayFrom(test, os.Getenv("GOFLAGS"))
}

// guardOverlayFrom is guardOverlay reading goFlags in place of the environment's GOFLAGS, so the tests
// that prove a named overlay is what the guards read can name one without test.Setenv, and run in parallel
// (#nxgt2ca).
func guardOverlayFrom(test *testing.T, goFlags string) map[string][]byte {
	test.Helper()

	refuseUnreachableOverlayFlag(test)

	overlayFile := ""
	for _, flag := range strings.Fields(goFlags) {
		if value, found := strings.CutPrefix(flag, "-overlay="); found {
			overlayFile = value
		} else if value, found := strings.CutPrefix(flag, "--overlay="); found {
			overlayFile = value
		}
	}
	if overlayFile == "" {
		return nil
	}
	overlay, readOverlayError := readOverlay(overlayFile)
	if readOverlayError != nil {
		test.Fatalf("GOFLAGS names the overlay %s, and the guards cannot read it: %v", overlayFile, readOverlayError)
	}
	return overlay
}

// readOverlay reads a `go build -overlay` file into file path to contents.
//
// Paths must be absolute. The go command resolves a relative one against its own working directory, and
// this binary runs in its package's directory and loads from the module root, so a relative path would
// name a different file at each layer. A deleted file ("" as its replacement) is refused too: the loader
// can replace a file's contents but has no way to say a file is absent, so the guard would read it.
func readOverlay(path string) (map[string][]byte, error) {
	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("the overlay path %s is relative; name it absolutely", path)
	}
	encoded, readError := os.ReadFile(path)
	if readError != nil {
		return nil, readError
	}
	var file struct {
		Replace map[string]string
	}
	if decodeError := json.Unmarshal(encoded, &file); decodeError != nil {
		return nil, fmt.Errorf("reading %s as an overlay: %w", path, decodeError)
	}
	overlay := map[string][]byte{}
	for target, replacement := range file.Replace {
		if !filepath.IsAbs(target) || (replacement != "" && !filepath.IsAbs(replacement)) {
			return nil, fmt.Errorf("the overlay entry %s -> %s is relative; name both absolutely", target, replacement)
		}
		if replacement == "" {
			return nil, fmt.Errorf(
				"the overlay deletes %s, which the loader cannot express, so the guard would still read it",
				target,
			)
		}
		contents, readError := os.ReadFile(replacement)
		if readError != nil {
			return nil, fmt.Errorf("reading the overlay's replacement for %s: %w", target, readError)
		}
		overlay[target] = contents
	}
	return overlay, nil
}

// refuseUnreachableOverlayFlag fails the guard when the `go test` that started this binary was given
// -overlay on its command line rather than through GOFLAGS. Best effort: where the parent's arguments
// cannot be read, nothing is refused.
func refuseUnreachableOverlayFlag(test *testing.T) {
	test.Helper()
	if runtime.GOOS == "windows" {
		return
	}
	output, outputError := exec.Command("ps", "-o", "args=", "-p", fmt.Sprint(os.Getppid())).Output()
	if outputError != nil {
		return
	}
	if overlayFlagOutsideGoFlags(string(output), os.Getenv("GOFLAGS")) {
		test.Fatalf("this guard was run by `%s`, whose -overlay builds the test binary and never reaches the guard, "+
			"so it would check the working tree; pass the overlay as GOFLAGS=-overlay=<absolute path> instead",
			strings.TrimSpace(string(output)))
	}
}

// overlayFlagOutsideGoFlags reports a go command line that carries -overlay when GOFLAGS does not.
func overlayFlagOutsideGoFlags(arguments string, goFlags string) bool {
	if strings.Contains(goFlags, "overlay") {
		return false
	}
	for _, argument := range strings.Fields(arguments) {
		if argument == "-overlay" ||
			argument == "--overlay" ||
			strings.HasPrefix(argument, "-overlay=") ||
			strings.HasPrefix(argument, "--overlay=") {
			return true
		}
	}
	return false
}

// writeOverlay writes an overlay file replacing target with contents, and returns the overlay file's path.
func writeOverlay(test *testing.T, target string, contents string) string {
	test.Helper()
	directory := test.TempDir()
	replacement := filepath.Join(directory, filepath.Base(target))
	if writeError := os.WriteFile(replacement, []byte(contents), 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	encoded, encodeError := json.Marshal(map[string]map[string]string{"Replace": {target: replacement}})
	if encodeError != nil {
		test.Fatal(encodeError)
	}
	overlayFile := filepath.Join(directory, "overlay.json")
	if writeError := os.WriteFile(overlayFile, encoded, 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	return overlayFile
}

// The creating-getter guard reads an overlay GOFLAGS names: a GetLocals call that exists only in the
// overlay is caught, and the working tree, which has none, is not what was read.
func TestTheCreatingGetterGuardReadsTheGoFlagsOverlay(test *testing.T) {
	test.Parallel()

	root, err := filepath.Abs("../..")
	if err != nil {
		test.Fatal(err)
	}
	path := filepath.Join(root, "internal/lint/rules/typescript/no_shadow.go")
	original, err := os.ReadFile(path)
	if err != nil {
		test.Fatal(err)
	}
	planted := strings.Replace(string(original), "range node.Locals()", "range ast.GetLocals(node)", 1)
	if planted == string(original) {
		test.Fatal("no-shadow no longer ranges over node.Locals(), so the plant missed; update the probe")
	}
	calls := creatingGetterCalls(test, guardOverlayFrom(test, "-overlay="+writeOverlay(test, path, planted)))
	if len(calls) != 1 || !strings.Contains(calls[0], "no_shadow.go") || !strings.HasSuffix(calls[0], "GetLocals") {
		test.Fatalf("a GetLocals call that exists only in the GOFLAGS overlay was not caught: %v", calls)
	}
}

// The TypeReach guard reads an overlay GOFLAGS names: a Shapes claim added only in the overlay, to a rule
// the scan already finds reading imported bodies, is caught.
func TestTheTypeReachGuardReadsTheGoFlagsOverlay(test *testing.T) {
	test.Parallel()

	root, err := filepath.Abs("../..")
	if err != nil {
		test.Fatal(err)
	}
	baseline := scanTypeReach(test, nil)
	claimed := map[string]bool{}
	for _, name := range baseline.claims {
		claimed[name] = true
	}
	// The first such rule by name, so every run plants into the same file.
	var candidates []string
	for name := range baseline.reason {
		if !claimed[name] {
			candidates = append(candidates, name)
		}
	}
	if len(candidates) == 0 {
		test.Fatal("no rule reads imported bodies without claiming Shapes, so there is nothing to plant a claim in")
	}
	slices.Sort(candidates)
	target := candidates[0]
	file := filepath.Join(root, "internal/lint/rules", baseline.reason[target])
	original, err := os.ReadFile(file)
	if err != nil {
		test.Fatal(err)
	}
	planted, err := plantShapesClaim(original)
	if err != nil {
		test.Fatalf("%s: %v", file, err)
	}
	scan := scanTypeReach(test, guardOverlayFrom(test, "-overlay="+writeOverlay(test, file, string(planted))))
	caught := false
	for _, name := range scan.claims {
		caught = caught || (name == target && scan.mayReadImportedBodies[name])
	}
	if !caught {
		test.Fatalf("a Shapes claim on %s that exists only in the GOFLAGS overlay was not caught", target)
	}
}

// plantShapesClaim returns a rule file whose rule.Rule literal claims TypeReach Shapes: the literal's own
// TypeReach replaced, or one added before its first element. Found by parsing, not by matching text, so
// a column-aligned `Name:` or any other spelling of the literal is planted the same way, and spelled with
// the literal's own package qualifier.
func plantShapesClaim(source []byte) ([]byte, error) {
	file, parseFileError := parser.ParseFile(token.NewFileSet(), "", source, 0)
	if parseFileError != nil {
		return nil, parseFileError
	}
	var literal *ast.CompositeLit
	var qualifier string
	ast.Inspect(file, func(node ast.Node) bool {
		if literal != nil {
			return false
		}
		candidate, ok := node.(*ast.CompositeLit)
		if !ok {
			return true
		}
		selector, isRule := ruleLiteralSelector(candidate.Type)
		if !isRule {
			return true
		}
		if identifier, ok := selector.X.(*ast.Ident); ok && len(candidate.Elts) > 0 {
			literal, qualifier = candidate, identifier.Name
		}
		return false
	})
	if literal == nil {
		return nil, fmt.Errorf("no rule.Rule literal with elements")
	}
	// Offsets in a file parsed alone start at 1.
	offset := func(position token.Pos) int { return int(position) - 1 }
	claim := qualifier + ".TypeReachShapes"
	for _, element := range literal.Elts {
		pair, ok := element.(*ast.KeyValueExpr)
		if key, isIdentifier := pair.Key.(*ast.Ident); ok && isIdentifier && key.Name == "TypeReach" {
			return slices.Concat(
				source[:offset(pair.Value.Pos())],
				[]byte(claim),
				source[offset(pair.Value.End()):],
			), nil
		}
	}
	first := offset(literal.Elts[0].Pos())
	return slices.Concat(source[:first], []byte("TypeReach: "+claim+",\n"), source[first:]), nil
}

// The plant works on every rule there is, read back the way the scan reads a claim, so the probe above
// cannot miss on whichever rule it happens to pick.
func TestAShapesClaimPlantsIntoEveryRuleFile(test *testing.T) {
	test.Parallel()

	root, err := filepath.Abs("../..")
	if err != nil {
		test.Fatal(err)
	}
	files, err := filepath.Glob(filepath.Join(root, "internal/lint/rules/*/*.go"))
	if err != nil {
		test.Fatal(err)
	}
	planted := 0
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		original, err := os.ReadFile(path)
		if err != nil {
			test.Fatal(err)
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), path, original, 0)
		if err != nil {
			test.Fatal(err)
		}
		name, _ := ruleLiteralTypeReach(parsed)
		if name == "" {
			continue
		}
		result, err := plantShapesClaim(original)
		if err != nil {
			test.Errorf("%s: %v", path, err)
			continue
		}
		reparsed, err := parser.ParseFile(token.NewFileSet(), path, result, 0)
		if err != nil {
			test.Errorf("%s: the planted file does not parse: %v", path, err)
			continue
		}
		if plantedName, claims := ruleLiteralTypeReach(reparsed); plantedName != name || !claims {
			test.Errorf(
				"%s: planted, the rule reads as %q claiming Shapes %v, want %q claiming it",
				path,
				plantedName,
				claims,
				name,
			)
		}
		planted++
	}
	if planted < 100 {
		test.Fatalf("planted into %d rule files, too few to be every rule", planted)
	}
}

// The command line `go test -overlay` is refused, since it cannot reach the guard, and GOFLAGS is not.
func TestAnOverlayFlagGoFlagsDoesNotCarryIsRefused(test *testing.T) {
	test.Parallel()

	for _, testCase := range []struct {
		arguments string
		goFlags   string
		refused   bool
	}{
		{"go test -overlay /tmp/x.json ./internal/guard", "", true},
		{"go test -overlay=/tmp/x.json ./internal/guard", "", true},
		{"go test ./internal/guard", "-overlay=/tmp/x.json", false},
		{"go test -overlay=/tmp/x.json ./internal/guard", "-overlay=/tmp/x.json", false},
		{"go test ./internal/guard", "", false},
	} {
		if got := overlayFlagOutsideGoFlags(testCase.arguments, testCase.goFlags); got != testCase.refused {
			test.Errorf(
				"%q with GOFLAGS %q: refused %v, want %v",
				testCase.arguments,
				testCase.goFlags,
				got,
				testCase.refused,
			)
		}
	}
}

// reactPragmaOwnFileCall is the rule's own-file pragma call, matched whatever the rule names its context
// parameter, so a rename of that parameter never breaks the fixture below.
var reactPragmaOwnFileCall = regexp.MustCompile(`rule\.IsReactPragmaBinding\([A-Za-z_][A-Za-z0-9_]*\.SourceFile,`)

// A pragma reader may inspect the home file's bodies only when home is the rule's own file. Runtime
// identifier checks alone cannot establish that argument at a call site; the scan must check it too.
func TestPragmaReaderTrustRequiresTheRulesOwnSourceFile(test *testing.T) {
	test.Parallel()
	root, err := filepath.Abs("../..")
	if err != nil {
		test.Fatal(err)
	}
	target := filepath.Join(root, "internal/lint/rules/react/checked_requires_onchange_or_readonly.go")
	original, err := os.ReadFile(target)
	if err != nil {
		test.Fatal(err)
	}
	matches := reactPragmaOwnFileCall.FindAllString(string(original), -1)
	if len(matches) != 1 {
		test.Fatalf("fixture must replace exactly one pragma call, found %d", len(matches))
	}
	before := matches[0]
	overlay := guardOverlay(test)
	planted := make(map[string][]byte, len(overlay)+1)
	for path, contents := range overlay {
		planted[path] = contents
	}
	planted[target] = []byte(
		strings.Replace(string(original), before, "rule.IsReactPragmaBinding(ast.GetSourceFileOfNode(callee),", 1),
	)
	baseline := scanTypeReach(test, overlay)
	unsafe := scanTypeReach(test, planted)
	for _, name := range []string{
		"react/no-adjacent-inline-elements",
		"react/no-multi-comp",
		"react/no-object-type-as-default-prop",
		"react/no-unstable-nested-components",
	} {
		if baseline.mayReadImportedBodies[name] {
			test.Errorf("%s: own-file pragma call refused", name)
		}
		if !unsafe.mayReadImportedBodies[name] {
			test.Errorf("%s: arbitrary-home pragma call trusted", name)
		}
	}
}
