package go_engine

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/cgo_testing"
	"github.com/system-inc/cohere/internal/go_engine/contract"
	"github.com/system-inc/cohere/internal/go_engine/rules"
)

// cgoStandardFile is the one cgo file of a small cgo package of the standard library, which each fake `GOROOT` has a
// copy of, so the package's directory is the fake's own, and everything else a link to the local installation.
const cgoStandardFile = "src/internal/runtime/cgobench/funcs.go"

// cgoStandardRun is what the engine said over the cgo package under one `GOROOT`: the lint record's crashes, and its
// findings, `GOROOT` spelled `$GOROOT` so two installations' findings compare.
type cgoStandardRun struct {
	crashes  []contract.Crash
	findings []string
}

// runOverCgoStandardPackage runs the engine, every rule on, under `--no-cache` and with its lint beside its load as
// overlaps says, over the cgo package of the `GOROOT` the process names, root, from root's src.
func runOverCgoStandardPackage(test *testing.T, root string, overlaps bool) cgoStandardRun {
	test.Helper()
	source := filepath.Join(root, "src")
	arguments := []string{"--contract", fmt.Sprint(contract.Version), "--root", source, "--no-fix", "--no-cache",
		filepath.Join(root, filepath.Dir(filepath.FromSlash(cgoStandardFile)))}
	var standardOutput bytes.Buffer
	var standardError bytes.Buffer
	exitCode := runOverlapping(arguments, nil, &standardOutput, &standardError, rules.All(), nil,
		embeddedStandardLibrary, overlaps, nil)
	if exitCode > exitFindings {
		test.Fatalf("under %s, exit %d; stderr:\n%s", root, exitCode, standardError.String())
	}
	var said cgoStandardRun
	linted := false
	for line := range strings.Lines(standardOutput.String()) {
		switch {
		case strings.HasPrefix(line, `{"kind":"lint",`):
			var record contract.LintRecord
			if decodeError := json.Unmarshal([]byte(line), &record); decodeError != nil {
				test.Fatalf("reading the lint record %s: %v", line, decodeError)
			}
			said.crashes, linted = record.Crashes, true
		case strings.HasPrefix(line, `{"kind":"finding",`):
			said.findings = append(said.findings, strings.ReplaceAll(strings.TrimSpace(line), root, "$GOROOT"))
		}
	}
	if !linted {
		test.Fatalf("under %s, no lint record in\n%s", root, standardOutput.String())
	}
	return said
}

// Two `GOROOT` copies of one toolchain at different paths, sharing one `GOCACHE`, share go's translation of a
// standard package's cgo files, since go keys it on their contents and not their directory, and its `//line`
// directives name the copy that made it. Once that copy is gone, the engine over the other copy reads the translation
// as if its own copy had made it (#gpmr5z0): every rule finishes, cgocall, which reads the raw files the directives
// name, and every rule that reads a file's bytes through the engine's own pass.ReadFile against the offsets its syntax
// holds, as the long-line rule does, and the findings are the first copy's, beside the load and after it alike.
//
// It needs a go that translates cgo, so it skips, named, where the toolchain has no cgo and under a runner's stand-in
// go, as Loom's, which refuses to compile.
//
// Not parallel: it sets the process's own `GOROOT` and `GOCACHE`, which every go command the engine runs reads.
func TestACgoTranslationSharedWithAGoneGorootReadsTheCurrentOne(test *testing.T) {
	cgo_testing.RequireTranslation(test)

	output, goEnvironmentError := exec.Command("go", "env", "GOROOT").Output()
	if goEnvironmentError != nil {
		test.Fatalf("reading GOROOT with go env: %v", goEnvironmentError)
	}
	local := strings.TrimSpace(string(output))
	first, second := mirroredGoRoot(test, local, cgoStandardFile, ""), mirroredGoRoot(test, local, cgoStandardFile, "")
	// Two mirrors' paths are of one length, and a rewrite to a path of another length is what moves a translation's
	// size, which a reader of the build cache's bytes would then disagree with.
	if renameError := os.Rename(first, first+"-first"); renameError != nil {
		test.Fatal(renameError)
	}
	first += "-first"
	test.Setenv("GOCACHE", test.TempDir())
	test.Setenv("GOTOOLCHAIN", "local")
	test.Setenv("CGO_ENABLED", "1")
	test.Setenv("GOROOT", first)
	underFirst := runOverCgoStandardPackage(test, first, true)
	if len(underFirst.crashes) > 0 || len(underFirst.findings) == 0 {
		test.Fatalf("under the first GOROOT want findings and no crash, got %d findings and crashes %+v",
			len(underFirst.findings), underFirst.crashes)
	}
	// Moved rather than deleted, which leaves the path its translation names gone all the same.
	if renameError := os.Rename(first, first+"-gone"); renameError != nil {
		test.Fatal(renameError)
	}
	test.Setenv("GOROOT", second)
	for _, overlaps := range []bool{true, false} {
		underSecond := runOverCgoStandardPackage(test, second, overlaps)
		if len(underSecond.crashes) > 0 {
			test.Errorf("under the second GOROOT, lint beside the load %t, the rules crashed: %+v", overlaps,
				underSecond.crashes)
		}
		if strings.Join(underSecond.findings, "\n") != strings.Join(underFirst.findings, "\n") {
			test.Errorf("under the second GOROOT, lint beside the load %t, the findings\n%s\nwant the first's\n%s",
				overlaps, strings.Join(underSecond.findings, "\n"), strings.Join(underFirst.findings, "\n"))
		}
	}
}
