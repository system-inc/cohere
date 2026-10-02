// Package patches holds the changes cohere carries on top of its pinned compiler, and proves each one
// is present in the binary rather than trusting that it was applied.
//
// The compiler is a submodule pinned to an upstream commit, so a fix that upstream has not merged
// cannot live inside it: a commit there exists only on the machine that made it, and every fresh clone,
// CI run and release would fail to fetch it. So the fix lives here as a patch file, applied to the
// submodule's working tree before building, by `command/cohere-patches`.
//
// # Why every patch carries a probe
//
// A list of patch files says what this repository intends, not what a binary contains. A build run
// without applying them compiles the stock compiler and would still embed the same list, so a version
// line read from the files would say "patched" over a checker that is not. Each patch therefore names a
// probe: a tiny program whose diagnostics differ between the stock and the patched checker. Verify
// runs it in-process against the checker this binary actually links, so the answer is measured on the
// artifact. The same probe is the Go test, which is what makes an unpatched build unable to pass.
//
// # Deleting a patch
//
// When upstream merges the fix and the pin moves past it, the patch stops applying, because its
// change is already there. `cohere-patches` reports that as already applied rather than as a failure,
// and that is the signal to delete the file and its entry below in the same commit.
package patches

import (
	"context"
	"embed"
	"fmt"
	"path"
	"sort"

	"github.com/system-inc/cohere/internal/types/program"
)

//go:embed *.patch
var files embed.FS

// Patch is one change carried on the pinned compiler.
type Patch struct {
	// File is the patch's file name in this directory, which is also its identity.
	File string

	// Task is the kingdom task that found and owns it.
	Task string

	// Upstream names the upstream commit the patch answers, so it can be deleted once that is fixed.
	Upstream string

	// Probe is a program that reports Expected diagnostics on a patched checker and a different
	// number on the stock one. Each file is checked with `--strict`.
	Probe map[string]string

	// Expected is how many diagnostics the probe reports when the patch is present.
	Expected int
}

// All is every patch, in the order they are applied.
var All = []Patch{
	{
		File:     "0001-reverse-mapped-inference-skips-private-members.patch",
		Task:     "#wpacpn2",
		Upstream: "microsoft/TypeScript 8239985a01 (#63932) exposed it; the defect is in resolveReverseMappedTypeMembers",
		// Stock checker since 8239985a01: TS2352 on the assertion. Patched, classic tsc 6.0.3 and
		// tsgo 7.0.0-dev.20260707.2: clean. Found on two Base authentication casts of this shape.
		Probe: map[string]string{
			"probe.ts": `class Entity {
    private secret = 1;
    markChanged(field: string): void {}
}
class Account extends Entity {
    emailAddress = '';
}
declare function getAccount<T extends object = object>(): Readonly<Account & T>;
export const account = getAccount() as Account;
`,
		},
		Expected: 0,
	},
}

// Contents returns a patch file's text exactly as embedded in this binary.
func Contents(file string) ([]byte, error) {
	return files.ReadFile(file)
}

// Result is what a probe measured.
type Result struct {
	Patch       Patch
	Diagnostics int
	Err         error
}

// Present reports whether the probe measured the patched behaviour.
func (result Result) Present() bool {
	return result.Err == nil && result.Diagnostics == result.Patch.Expected
}

// Verify runs every probe against the checker this binary links.
//
// The probe is built entirely in memory through the overlay, with an explicit file list so nothing
// lists a directory, which means it reads nothing from the project and writes nothing anywhere.
func Verify(ctx context.Context, directory string) []Result {
	results := make([]Result, 0, len(All))
	for _, patch := range All {
		results = append(results, verifyOne(ctx, directory, patch))
	}
	return results
}

func verifyOne(ctx context.Context, directory string, patch Patch) Result {
	if _, err := files.ReadFile(patch.File); err != nil {
		return Result{Patch: patch, Err: fmt.Errorf("patch file %s is listed but not embedded", patch.File)}
	}

	root := path.Join(directory, ".cohere-patch-probe")
	names := make([]string, 0, len(patch.Probe))
	overlay := make(map[string]string, len(patch.Probe)+1)
	for name, contents := range patch.Probe {
		names = append(names, fmt.Sprintf("%q", name))
		overlay[path.Join(root, name)] = contents
	}
	sort.Strings(names)
	fileList := ""
	for index, name := range names {
		if index > 0 {
			fileList += ", "
		}
		fileList += name
	}
	configFileName := path.Join(root, "tsconfig.json")
	overlay[configFileName] = `{ "compilerOptions": { "strict": true, "target": "es2022", "module": "esnext", ` +
		`"noEmit": true, "skipLibCheck": true }, "files": [` + fileList + `] }`

	graph, err := program.Build(program.Options{
		ConfigFileName:   configFileName,
		CurrentDirectory: root,
		SingleThreaded:   true,
		Overlay:          overlay,
	})
	if err != nil {
		return Result{Patch: patch, Err: err}
	}
	return Result{Patch: patch, Diagnostics: len(graph.AllDiagnostics(ctx))}
}

// Describe renders the results as version lines, one per patch.
//
// A patch that is listed but not measured in this binary is printed as missing rather than omitted,
// for the same reason the rest of the provenance prints its unknowns: a missing line and a missing
// patch read identically in a pasted bug report while meaning very different things.
func Describe(results []Result) []string {
	if len(results) == 0 {
		return []string{"  compiler patches: none"}
	}
	lines := []string{fmt.Sprintf("  compiler patches: %d", len(results))}
	for _, result := range results {
		state := "present (measured in this binary)"
		switch {
		case result.Err != nil:
			state = "UNVERIFIED: " + result.Err.Error()
		case !result.Present():
			state = fmt.Sprintf("MISSING: probe reported %d diagnostics, expected %d; this binary was built "+
				"from an unpatched compiler, run command/cohere-patches and rebuild",
				result.Diagnostics, result.Patch.Expected)
		}
		lines = append(lines, fmt.Sprintf("    %s  %s", result.Patch.File, state))
	}
	return lines
}
