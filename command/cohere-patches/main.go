// Command cohere-patches applies the patches in `patches/` to the vendored compiler's working tree.
//
//	go run ./command/cohere-patches          apply anything not yet applied
//	go run ./command/cohere-patches -check   report, write nothing, exit nonzero unless all applied
//
// Run it from the repository root, after cloning and after every submodule bump.
//
// # The exit code is a contract
//
// The release build runs `-check` before cross-compiling and refuses to release on a nonzero exit,
// because the six target binaries cannot be run on the machine building them to ask their
// `--version`. So `-check` exiting nonzero whenever any patch is not applied, the case where a patch
// applies in neither direction included, is what stands between a skipped step and a release of the
// stock compiler. main_test.go pins it; tell @system_cohere_release before changing it.
//
// # It refuses rather than skips
//
// A patch that applies in neither direction stops the run with the patch named. Skipping it would
// build the stock compiler under a binary that still lists the patch.
//
// # Three states, because git apply alone sees two
//
// A patch whose reverse applies is either one this tool applied, or one that upstream has since
// merged, so that the pinned commit already contains the change. git apply cannot tell those apart.
// The difference is whether the patched files still differ from the submodule's own HEAD: ours do,
// upstream's do not. The second case is reported loudly, because it is the signal to delete the
// patch, and nothing else would ever say so.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/system-inc/cohere/patches"
)

func main() {
	os.Exit(run(os.Args[1:], patches.All, os.Stdout, os.Stderr))
}

// run is the whole command, returning its exit code, so the contract above can be tested directly.
func run(arguments []string, all []patches.Patch, standardOutput io.Writer, standardError io.Writer) int {
	flags := flag.NewFlagSet("cohere-patches", flag.ContinueOnError)
	flags.SetOutput(standardError)
	checkOnly := flags.Bool("check", false, "report the state of every patch and write nothing")
	root := flags.String("root", ".", "the cohere repository root")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	fail := func(format string, values ...any) int {
		fmt.Fprintf(standardError, "cohere-patches: "+format+"\n", values...)
		return 1
	}

	submodule := filepath.Join(*root, "TypeScript")
	if _, err := os.Stat(filepath.Join(submodule, ".git")); err != nil {
		return fail("no vendored compiler at %s: run `git submodule update --init` first", submodule)
	}

	notApplied := 0
	for _, patch := range all {
		contents, err := patches.Contents(patch.File)
		if err != nil {
			return fail("%s is listed but not embedded: %v", patch.File, err)
		}

		switch {
		case gitApply(submodule, contents, "--reverse", "--check") == nil:
			if differsFromHead(submodule, contents) {
				fmt.Fprintf(standardOutput, "applied    %s\n", patch.File)
			} else {
				fmt.Fprintf(standardOutput, "UPSTREAM   %s: the pinned compiler already contains this "+
					"change. Delete the patch file and its entry in patches/patches.go in one commit.\n",
					patch.File)
			}
		case gitApply(submodule, contents, "--check") == nil:
			if *checkOnly {
				fmt.Fprintf(standardOutput, "missing    %s\n", patch.File)
				notApplied++
				continue
			}
			if err := gitApply(submodule, contents); err != nil {
				return fail("%s passed its check and then failed to apply: %v", patch.File, err)
			}
			fmt.Fprintf(standardOutput, "applied    %s (now)\n", patch.File)
		default:
			return fail("%s no longer applies to the vendored compiler at %s. If upstream fixed this, "+
				"delete the patch and its entry in patches/patches.go; otherwise rewrite it against the "+
				"new pin. The build must not continue on the stock compiler.",
				patch.File, submoduleHead(submodule))
		}
	}

	if notApplied > 0 {
		return fail("%d not applied; run without -check to apply", notApplied)
	}
	return 0
}

func gitApply(submodule string, contents []byte, arguments ...string) error {
	command := exec.Command("git", append([]string{"-C", submodule, "apply"}, append(arguments, "-")...)...)
	command.Stdin = bytes.NewReader(contents)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

var patchedFile = regexp.MustCompile(`(?m)^\+\+\+ b/(.+)$`)

// differsFromHead reports whether any file the patch touches differs from the submodule's HEAD.
func differsFromHead(submodule string, contents []byte) bool {
	for _, match := range patchedFile.FindAllSubmatch(contents, -1) {
		command := exec.Command("git", "-C", submodule, "diff", "--quiet", "HEAD", "--", string(match[1]))
		if command.Run() != nil {
			return true
		}
	}
	return false
}

func submoduleHead(submodule string) string {
	output, err := exec.Command("git", "-C", submodule, "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		return "an unknown commit"
	}
	return strings.TrimSpace(string(output))
}
