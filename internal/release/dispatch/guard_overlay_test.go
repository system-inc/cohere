package dispatch

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// guardOverlay is the overlay the guards here read the rule packages through: the one GOFLAGS names, as
// file path to contents, or nil when GOFLAGS names none.
//
// The guards load the rule packages with go/packages, which runs `go list` and then parses every file
// itself, from disk. `go test -overlay x.json` reaches neither: the flag builds the test binary and
// stops there, and nothing in the binary can see it (measured: no build setting records it, and the
// environment is untouched). So a guard run that way checked the working tree while the command line
// said it checked the overlay, and two wrong "dispatch green" claims went through on it. GOFLAGS is
// inherited by the test binary and by the `go list` go/packages starts, so `GOFLAGS=-overlay=x.json`
// is the spelling every layer reads; this hands its contents to go/packages' own parser.
//
// The spelling that cannot work is refused rather than ignored, when it can be seen: on Unix the `go`
// command that started this binary is asked for its arguments, and an -overlay there that GOFLAGS does
// not carry fails the guard by name.
func guardOverlay(t *testing.T) map[string][]byte {
	t.Helper()

	refuseUnreachableOverlayFlag(t)

	overlayFile := ""
	for _, flag := range strings.Fields(os.Getenv("GOFLAGS")) {
		if value, found := strings.CutPrefix(flag, "-overlay="); found {
			overlayFile = value
		} else if value, found := strings.CutPrefix(flag, "--overlay="); found {
			overlayFile = value
		}
	}
	if overlayFile == "" {
		return nil
	}
	overlay, err := readOverlay(overlayFile)
	if err != nil {
		t.Fatalf("GOFLAGS names the overlay %s, and the guards cannot read it: %v", overlayFile, err)
	}
	return overlay
}

// readOverlay reads a `go build -overlay` file into file path to contents.
//
// Paths must be absolute. The go command resolves a relative one against its own working directory, and
// this binary runs in its package's directory and loads from the module root, so a relative path would
// name a different file at each layer. A deleted file ("" as its replacement) is refused too: go/packages
// can replace a file's contents but has no way to say a file is absent, so the guard would read it.
func readOverlay(path string) (map[string][]byte, error) {
	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("the overlay path %s is relative; name it absolutely", path)
	}
	encoded, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var file struct {
		Replace map[string]string
	}
	if err := json.Unmarshal(encoded, &file); err != nil {
		return nil, fmt.Errorf("reading %s as an overlay: %w", path, err)
	}
	overlay := map[string][]byte{}
	for target, replacement := range file.Replace {
		if !filepath.IsAbs(target) || (replacement != "" && !filepath.IsAbs(replacement)) {
			return nil, fmt.Errorf("the overlay entry %s -> %s is relative; name both absolutely", target, replacement)
		}
		if replacement == "" {
			return nil, fmt.Errorf("the overlay deletes %s, which go/packages cannot express, so the guard would still read it", target)
		}
		contents, err := os.ReadFile(replacement)
		if err != nil {
			return nil, fmt.Errorf("reading the overlay's replacement for %s: %w", target, err)
		}
		overlay[target] = contents
	}
	return overlay, nil
}

// refuseUnreachableOverlayFlag fails the guard when the `go test` that started this binary was given
// -overlay on its command line rather than through GOFLAGS. Best effort: where the parent's arguments
// cannot be read, nothing is refused.
func refuseUnreachableOverlayFlag(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		return
	}
	output, err := exec.Command("ps", "-o", "args=", "-p", fmt.Sprint(os.Getppid())).Output()
	if err != nil {
		return
	}
	if overlayFlagOutsideGoFlags(string(output), os.Getenv("GOFLAGS")) {
		t.Fatalf("this guard was run by `%s`, whose -overlay builds the test binary and never reaches the guard, "+
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
		if argument == "-overlay" || argument == "--overlay" || strings.HasPrefix(argument, "-overlay=") || strings.HasPrefix(argument, "--overlay=") {
			return true
		}
	}
	return false
}

// writeOverlay writes an overlay file replacing target with contents, and returns the overlay file's path.
func writeOverlay(t *testing.T, target string, contents string) string {
	t.Helper()
	directory := t.TempDir()
	replacement := filepath.Join(directory, filepath.Base(target))
	if err := os.WriteFile(replacement, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(map[string]map[string]string{"Replace": {target: replacement}})
	if err != nil {
		t.Fatal(err)
	}
	overlayFile := filepath.Join(directory, "overlay.json")
	if err := os.WriteFile(overlayFile, encoded, 0o644); err != nil {
		t.Fatal(err)
	}
	return overlayFile
}

// The creating-getter guard reads an overlay GOFLAGS names: a GetLocals call that exists only in the
// overlay is caught, and the working tree, which has none, is not what was read.
func TestTheCreatingGetterGuardReadsTheGoFlagsOverlay(t *testing.T) {
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "internal/lint/rules/typescript/no_shadow.go")
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	planted := strings.Replace(string(original), "range node.Locals()", "range ast.GetLocals(node)", 1)
	if planted == string(original) {
		t.Fatal("no-shadow no longer ranges over node.Locals(), so the plant missed; update the probe")
	}
	t.Setenv("GOFLAGS", "-overlay="+writeOverlay(t, path, planted))

	calls := creatingGetterCalls(t, guardOverlay(t))
	if len(calls) != 1 || !strings.Contains(calls[0], "no_shadow.go") || !strings.HasSuffix(calls[0], "GetLocals") {
		t.Fatalf("a GetLocals call that exists only in the GOFLAGS overlay was not caught: %v", calls)
	}
}

// The TypeReach guard reads an overlay GOFLAGS names: a Shapes claim added only in the overlay, to a rule
// the scan already finds reading imported bodies, is caught.
func TestTheTypeReachGuardReadsTheGoFlagsOverlay(t *testing.T) {
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	baseline := scanTypeReach(t, nil)
	claimed := map[string]bool{}
	for _, name := range baseline.claims {
		claimed[name] = true
	}
	target, file := "", ""
	for name, reader := range baseline.reason {
		if !claimed[name] {
			target, file = name, filepath.Join(root, "internal/lint/rules", reader)
			break
		}
	}
	if target == "" {
		t.Fatal("no rule reads imported bodies without claiming Shapes, so there is nothing to plant a claim in")
	}
	original, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	nameField := fmt.Sprintf("Name: %q,", target)
	if strings.Contains(string(original), "TypeReach:") || !strings.Contains(string(original), nameField) {
		t.Fatalf("%s does not spell its rule as the plant expects; update the probe", file)
	}
	planted := strings.Replace(string(original), nameField, nameField+"\n\t\tTypeReach: rule.TypeReachShapes,", 1)
	t.Setenv("GOFLAGS", "-overlay="+writeOverlay(t, file, planted))

	scan := scanTypeReach(t, guardOverlay(t))
	caught := false
	for _, name := range scan.claims {
		caught = caught || (name == target && scan.mayReadImportedBodies[name])
	}
	if !caught {
		t.Fatalf("a Shapes claim on %s that exists only in the GOFLAGS overlay was not caught", target)
	}
}

// The command line `go test -overlay` is refused, since it cannot reach the guard, and GOFLAGS is not.
func TestAnOverlayFlagGoFlagsDoesNotCarryIsRefused(t *testing.T) {
	for _, testCase := range []struct {
		arguments string
		goFlags   string
		refused   bool
	}{
		{"go test -overlay /tmp/x.json ./internal/release/dispatch", "", true},
		{"go test -overlay=/tmp/x.json ./internal/release/dispatch", "", true},
		{"go test ./internal/release/dispatch", "-overlay=/tmp/x.json", false},
		{"go test -overlay=/tmp/x.json ./internal/release/dispatch", "-overlay=/tmp/x.json", false},
		{"go test ./internal/release/dispatch", "", false},
	} {
		if got := overlayFlagOutsideGoFlags(testCase.arguments, testCase.goFlags); got != testCase.refused {
			t.Errorf("%q with GOFLAGS %q: refused %v, want %v", testCase.arguments, testCase.goFlags, got, testCase.refused)
		}
	}
}
