//go:build unix

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/testing/executable_testing"
	"github.com/system-inc/cohere/internal/testing/needs"
)

// plantedGo stands in for a shared box's go: it answers go env, and records every go mod download, go mod verify and
// go test it is asked for in GATE_LOG, with the Go settings it ran under (a go mod's its module cache and proxy) and
// whether its Go cache was empty, so a test can read what the gate asked of it. GATE_VERIFY_FAILS makes go mod verify
// fail as a tampered module cache would, GATE_DOWNLOAD_FAILS makes go mod download fail as a box cut off from the
// module proxy would, and GATE_RACE_FAILS makes the -race run of the package it names fail as one that found a race
// would.
const plantedGo = `#!/bin/sh
case "$1" in
env) [ "$2" = GOOS ] && echo linux; [ "$2" = GOARCH ] && echo amd64; exit 0 ;;
mod)
  echo "$2 | modcache ${GOMODCACHE:-} | proxy ${GOPROXY:-} | sum database ${GOSUMDB:-}" >> "$GATE_LOG"
  if [ "$2" = download ] && [ -n "$GATE_DOWNLOAD_FAILS" ]; then
    echo "example.com/module@v1.0.0: no route to the proxy"; exit 1; fi
  if [ "$2" = verify ] && [ -n "$GATE_VERIFY_FAILS" ]; then echo "example.com/module v1.0.0: dir has been modified"
    exit 1; fi
  [ "$2" = verify ] && echo "all modules verified"; exit 0 ;;
test)
  empty=no; [ -z "$(ls -A "$GOCACHE")" ] && empty=yes
  echo "test $* | cache $GOCACHE empty $empty | toolchain $GOTOOLCHAIN | goenv $GOENV | goflags $GOFLAGS | path $PATH" >> "$GATE_LOG"
  if [ -n "$GATE_RACE_FAILS" ]; then case " $* " in *" -race"*" $GATE_RACE_FAILS "*)
    echo '{"Action":"fail","Package":"example.com/module"}'; exit 1 ;; esac; fi
  echo '{"Action":"pass","Package":"example.com/module"}' ;;
esac
`

// toolchainDigest is sharedBoxGate's digest of a toolchain tree, computed here: every regular file by its path from
// the root, "./" first, in byte order, each as sha256sum prints it, and that list's sha256.
func toolchainDigest(test *testing.T, root string) string {
	test.Helper()
	sums := map[string]string{}
	walkError := filepath.WalkDir(root, func(path string, entry fs.DirEntry, pathError error) error {
		if pathError != nil || !entry.Type().IsRegular() {
			return pathError
		}
		contents, readError := os.ReadFile(path)
		if readError != nil {
			return readError
		}
		relative, relativeError := filepath.Rel(root, path)
		if relativeError != nil {
			return relativeError
		}
		sum := sha256.Sum256(contents)
		sums["./"+filepath.ToSlash(relative)] = hex.EncodeToString(sum[:])
		return nil
	})
	if walkError != nil {
		test.Fatal(walkError)
	}
	paths := make([]string, 0, len(sums))
	for path := range sums {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	listing := strings.Builder{}
	for _, path := range paths {
		listing.WriteString(sums[path] + "  " + path + "\n")
	}
	sum := sha256.Sum256([]byte(listing.String()))
	return hex.EncodeToString(sum[:])
}

// withoutModuleCache is environment without `GOMODCACHE`, so a script run in a test's environment reads no module set
// laid for it unless the test lays one, whatever the machine running the test names.
func withoutModuleCache(environment []string) []string {
	return slices.DeleteFunc(environment, func(variable string) bool {
		return strings.HasPrefix(variable, "GOMODCACHE=")
	})
}

// A shared box's gate trusts nothing the box kept (#adbkt5t). Its toolchain must digest to the pinned one before any
// of it runs, its module cache must pass go mod verify before any test, and every test runs with -count=1 on a Go
// cache made empty for the job, with the account's Go settings left unread, an account's `GOMODCACHE` among them, so go
// mod download and go mod verify read Go's default cache with the proxy as Go sets it, and the cache is gone after.
// Shown by running the gate's own script with bash against a planted toolchain whose go records what it is asked. The
// land job's module set, the one `GOMODCACHE` it reads, is TestTheLandJobsCacheIsKeptAndDropsCountOne's.
func TestASharedBoxGateTrustsNothingTheBoxKept(test *testing.T) {
	test.Parallel()
	needs.Tool(test, "bash", "the gate is a bash script")
	needs.Tool(test, "sha256sum", "the gate digests the toolchain with it, as the boxes do")
	for _, row := range []struct {
		name string
		// plant changes the toolchain after its digest is pinned, or the environment the gate runs in.
		plant func(test *testing.T, goroot string, environment []string) []string
		// refusal is what the gate must say when it refuses, empty when it runs the tests.
		refusal string
		// node is the box's node: "pinned" by the digest the gate is handed, "absent", or "swapped" after its pin.
		node string
		// raceFails is whether the gate fails for its -race runs alone, every one of them asked first.
		raceFails bool
		// accountModules is whether the account names a module cache in `GOMODCACHE`, which outside the land job is
		// never read.
		accountModules bool
	}{
		{name: "a toolchain as pinned runs the tests", node: "pinned"},
		{name: "an account's module cache is never read outside the land job", node: "pinned", accountModules: true},
		{
			name: "the plain tokens' -race run failing fails the gate, after the token with a run has run",
			node: "pinned",
			plant: func(test *testing.T, goroot string, environment []string) []string {
				return append(environment, "GATE_RACE_FAILS=./internal/edit")
			},
			raceFails: true,
		},
		{
			name: "the -race run of the token with a run failing fails the gate",
			node: "pinned",
			plant: func(test *testing.T, goroot string, environment []string) []string {
				return append(environment, "GATE_RACE_FAILS=./internal/go_language")
			},
			raceFails: true,
		},
		{name: "a box without node runs the tests with no node on the path", node: "absent"},
		{
			name:    "a node changed after its pin is refused before any test",
			node:    "swapped",
			refusal: "the node at",
		},
		{
			name: "a toolchain changed after its pin is refused before it runs",
			plant: func(test *testing.T, goroot string, environment []string) []string {
				writeFixtureFile(test, filepath.Join(goroot, "src", "fmt", "print.go"), "package fmt // swapped\n")
				return environment
			},
			refusal: "digests to",
		},
		{
			name: "a toolchain holding a link is refused",
			plant: func(test *testing.T, goroot string, environment []string) []string {
				if linkError := os.Symlink(
					"print.go",
					filepath.Join(goroot, "src", "fmt", "linked.go"),
				); linkError != nil {
					test.Fatal(linkError)
				}
				return environment
			},
			refusal: "holds a link",
		},
		{
			name: "a module cache that fails go mod verify is refused before any test",
			plant: func(test *testing.T, goroot string, environment []string) []string {
				return append(environment, "GATE_VERIFY_FAILS=1")
			},
			refusal: "go mod verify failed: example.com/module v1.0.0: dir has been modified",
		},
		{
			name: "a module the box can't fetch is refused before any test",
			plant: func(test *testing.T, goroot string, environment []string) []string {
				return append(environment, "GATE_DOWNLOAD_FAILS=1")
			},
			refusal: "go mod download failed: example.com/module@v1.0.0: no route to the proxy",
		},
	} {
		test.Run(row.name, func(test *testing.T) {
			test.Parallel()
			goroot := filepath.Join(test.TempDir(), "go")
			writeFixtureFile(test, filepath.Join(goroot, "src", "fmt", "print.go"), "package fmt\n")
			if writeError := os.MkdirAll(filepath.Join(goroot, "bin"), 0o755); writeError != nil {
				test.Fatal(writeError)
			}
			if writeError := executable_testing.WriteFile(filepath.Join(goroot, "bin", "go"), []byte(plantedGo),
				0o755); writeError != nil {
				test.Fatal(writeError)
			}
			digest := toolchainDigest(test, goroot)

			tree := test.TempDir()
			writeFixtureFile(test, filepath.Join(tree, "go.mod"), "module example.com/module\n\ngo 1.27\n")
			for _, arguments := range [][]string{
				{"init", "-q"},
				{"add", "."},
				{"-c", "user.name=test", "-c", "user.email=test@example.com", "commit", "-q", "-m", "a module"},
			} {
				git := exec.Command("git", arguments...)
				git.Dir = tree
				if output, gitError := git.CombinedOutput(); gitError != nil {
					test.Fatalf("git %v: %v\n%s", arguments, gitError, output)
				}
			}
			// A file the token's run names as a glob, which an unquoted run would expand to in the tree.
			writeFixtureFile(test, filepath.Join(tree, "AtOnce|SideBy.planted"), "")
			log := filepath.Join(test.TempDir(), "asked")
			temporary := test.TempDir()
			// The account's own Go settings, which the gate must not read: a cache, flags, a toolchain switch and a
			// proxy.
			environment := append(withoutModuleCache(os.Environ()), "GATE_LOG="+log, "TMPDIR="+temporary,
				"GOCACHE="+test.TempDir(), "GOFLAGS=-count=5", "GOTOOLCHAIN=auto",
				"GOENV="+filepath.Join(test.TempDir(), "env"), "GOPROXY=https://proxy.example.com")
			if row.accountModules {
				environment = append(environment, "GOMODCACHE="+filepath.Join(test.TempDir(), "account modules"))
			}
			if row.plant != nil {
				environment = row.plant(test, goroot, environment)
			}
			// The box's node, pinned by its digest unless the row has it absent or swapped after the pin.
			node := filepath.Join(test.TempDir(), "node")
			nodeDigest := strings.Repeat("0", 64)
			if row.node != "absent" {
				writeFixtureFile(test, filepath.Join(node, "bin", "node"), "a pinned node\n")
				sum := sha256.Sum256([]byte("a pinned node\n"))
				nodeDigest = hex.EncodeToString(sum[:])
			}
			if row.node == "swapped" {
				writeFixtureFile(test, filepath.Join(node, "bin", "node"), "a swapped node\n")
			}
			script := "set -euo pipefail\n" +
				`goroot="$1"; digest="$2"; node="$3"; nodeDigest="$4"; races="$5"; shift 5` +
				"\n" +
				sharedBoxGate
			// A plain token and one with a run, which reaches go test whole: its `.*` is never globbed to the file
			// planted above.
			gate := exec.Command("bash", "-c", script, "gate", goroot, digest, node, nodeDigest,
				"./internal/edit,./internal/go_language@AtOnce|SideBy.*", "./...")
			gate.Dir, gate.Env = tree, environment
			output, gateError := gate.CombinedOutput()
			// Ignored: a gate that refused before any go ran asked nothing, and its log was never written.
			asked, _ := os.ReadFile(log)

			if row.refusal != "" {
				if gateError == nil || !strings.Contains(string(output), sharedBoxRefusedMarker+"") ||
					!strings.Contains(string(output), row.refusal) {
					test.Fatalf("the gate did not refuse, saying %q (%v):\n%s", row.refusal, gateError, output)
				}
				if strings.Contains(string(asked), "test ") {
					test.Errorf("the gate refused, but ran tests first:\n%s", asked)
				}
				return
			}
			if (gateError != nil) != row.raceFails {
				test.Fatalf("the gate failed: %v, want it failed %t\n%s", gateError, row.raceFails, output)
			}
			lines := strings.Split(strings.TrimSpace(string(asked)), "\n")
			// go mod reads Go's default cache with the proxy Go sets, the account's never read.
			modules, said := " | modcache  | proxy  | sum database ", boxModulesMarker+
				"Go's default cache, from the proxy\n"
			if len(lines) != 5 || lines[0] != "download"+modules || lines[1] != "verify"+modules {
				test.Fatalf("the gate asked for %q, want go mod download and go mod verify with %q, then the "+
					"module's tests and the -race run's two go tests", lines, modules)
			}
			if !strings.Contains(string(output), said) {
				test.Errorf("the gate didn't say %q:\n%s", said, output)
			}
			// Past the download, the log reads as the checks below index it: verify, the tests, the -race runs.
			lines = lines[1:]
			for _, line := range lines[1:] {
				for _, want := range []string{"-count=1", "-timeout " + sharedBoxTestTimeout,
					"empty yes", "toolchain local", "goenv off",
					"goflags -buildvcs=false -trimpath", "cache " + temporary} {
					if !strings.Contains(line, want) {
						test.Errorf("a go test the gate asked for lacks %q: %s", want, line)
					}
				}
			}
			// The log opens each go test's line with `test`, then its arguments whole, their own `test` first.
			if !strings.HasPrefix(lines[2], "test test -count=1 -timeout "+sharedBoxTestTimeout+
				" -json -race ./internal/edit |") {
				test.Errorf("the -race run of the plain tokens is not of the race list: %s", lines[2])
			}
			// A token with a run is a go test of its own, its run whole, after the plain tokens' and through the same
			// flags.
			if !strings.HasPrefix(lines[3], "test test -count=1 -timeout "+sharedBoxTestTimeout+
				" -json -race -run AtOnce|SideBy.* ./internal/go_language |") {
				test.Errorf("the -race run of the token with a run is not its own go test: %s", lines[3])
			}
			if !strings.Contains(string(output), boxRaceMarker+"\n") {
				test.Errorf("the gate printed no %q before its -race run:\n%s", boxRaceMarker, output)
			}
			// A pinned node is on the tests' path, before the system's own; an absent one is nowhere on it.
			// The path is the line's last field, so it is read whole, the system's sbin directories included.
			onPath := strings.HasSuffix(lines[1], "path "+goroot+"/bin:"+node+"/bin:/usr/bin:/bin:/usr/sbin:/sbin")
			offPath := strings.HasSuffix(lines[1], "path "+goroot+"/bin:/usr/bin:/bin:/usr/sbin:/sbin")
			if (row.node == "pinned" && !onPath) || (row.node == "absent" && !offPath) {
				test.Errorf("with the box's node %s, the tests ran on the wrong path: %s", row.node, lines[1])
			}
			left, readError := os.ReadDir(temporary)
			if readError != nil {
				test.Fatal(readError)
			}
			if len(left) != 0 {
				test.Errorf("the gate left its Go cache behind: %v", left)
			}
		})
	}
}

// A shared box's gate handed a path that holds nothing but the toolchain's own directory, as a land job's sandbox hands
// it one without /sbin, still digests the toolchain and runs the tests: the system's directories go last on the path
// before the first check (sharedBoxSystemPathAfter), so sha256sum, find and sort are found, and set -e never ends the
// script before it says a word (#5wj23dd).
func TestASharedBoxGateDigestsOnAPathWithoutTheSystemsDirectories(test *testing.T) {
	test.Parallel()
	bash := needs.Tool(test, "bash", "the gate is a bash script")
	needs.Tool(test, "sha256sum", "the gate digests the toolchain with it, as the boxes do")
	goroot := filepath.Join(test.TempDir(), "go")
	writeFixtureFile(test, filepath.Join(goroot, "src", "fmt", "print.go"), "package fmt\n")
	if makeError := os.MkdirAll(filepath.Join(goroot, "bin"), 0o755); makeError != nil {
		test.Fatal(makeError)
	}
	if writeError := executable_testing.WriteFile(filepath.Join(goroot, "bin", "go"), []byte(plantedGo),
		0o755); writeError != nil {
		test.Fatal(writeError)
	}
	tree := test.TempDir()
	writeFixtureFile(test, filepath.Join(tree, "go.mod"), "module example.com/module\n\ngo 1.27\n")
	log := filepath.Join(test.TempDir(), "asked")
	script := "set -euo pipefail\n" + `goroot="$1"; digest="$2"; node="$3"; nodeDigest="$4"; races="$5"; shift 5` +
		"\n" + sharedBoxGate
	gate := exec.Command(bash, "-c", script, "gate", goroot, toolchainDigest(test, goroot),
		filepath.Join(tree, "no-node"), strings.Repeat("0", 64), "", "./...")
	gate.Dir = tree
	gate.Env = append(withoutModuleCache(os.Environ()), "PATH="+filepath.Join(goroot, "bin"), "GATE_LOG="+log,
		"TMPDIR="+test.TempDir())
	output, gateError := gate.CombinedOutput()
	if gateError != nil || !strings.Contains(string(output), boxStepMarker+"toolchain ") {
		test.Fatalf("on a path of the toolchain alone the gate didn't digest it (%v):\n%s", gateError, output)
	}
	if asked := string(readMaybeMissing(test, log)); !strings.Contains(asked, "test test -count=1 ") {
		test.Errorf("on a path of the toolchain alone the gate ran no tests:\n%s", asked)
	}
}

// Kirk's Mac's runs keep Go's test cache, which is keyed honestly there: the exception is for shared boxes alone, so no
// go test Kirk's Mac runs carries -count. Shown through a landing with the gate off, module and -race run, whose go
// records its arguments: since no box's skips run again on Kirk's Mac, the escape is the one road that runs tests
// there. Its -race run is the box's: the plain packages in one go test, and a package whose entry names a run in one
// of its own, its run whole.
func TestTheMacsGateRunsKeepTheTestCache(test *testing.T) {
	test.Parallel()
	fixture := newLandFixture(test)
	asked := filepath.Join(test.TempDir(), "asked")
	recorder := "#!/bin/sh\necho \"$*\" >> '" + asked + "'\n"
	if writeError := executable_testing.WriteFile(filepath.Join(filepath.Dir(fixture.log), "go"),
		[]byte(recorder), 0o755); writeError != nil {
		test.Fatal(writeError)
	}
	worktree := fixture.worktree("a")
	writeFixtureFile(test, filepath.Join(worktree, "go.mod"), "module example.com/fixture\n\ngo 1.27\n")
	writeFixtureFile(test, filepath.Join(worktree, "internal", "edit", "edit_test.go"),
		"package edit\n\nimport \"testing\"\n\nfunc TestEdit(test *testing.T) {}\n")
	// A package at the -race list's entry that names a run, so the -race run asks for it with that run.
	goLanguage := raceGatedEntry(test, "internal/go_language")
	writeFixtureFile(test, filepath.Join(worktree, "internal", "go_language", "main_test.go"),
		"package go_language\n\nimport \"testing\"\n\nfunc TestChecker(test *testing.T) {}\n")
	fixture.git(worktree, "add", ".")
	fixture.git(worktree, "commit", "-q", "-m", fixtureSubject("a module"))
	fixture.git(worktree, "merge", "-q", "--no-edit", "-m", "Merge main", "main")
	landing := exec.Command(fixture.wrapper, "land", "--message", "a module tested")
	landing.Dir, landing.Env = worktree, append(fixture.environment, gateVariable+"="+gateOffSetting)
	output, landError := landing.CombinedOutput()
	if landError != nil {
		test.Fatalf("the landing failed: %v\n%s", landError, output)
	}
	recorded, readError := os.ReadFile(asked)
	if readError != nil {
		test.Fatal(readError)
	}
	tests := []string{}
	for _, line := range strings.Split(strings.TrimSpace(string(recorded)), "\n") {
		if !strings.HasPrefix(line, "test") {
			continue
		}
		tests = append(tests, line)
		if strings.Contains(line, "-count") {
			test.Errorf("Kirk's Mac ran a go test with -count, which throws its honest cache away: go %s", line)
		}
	}
	if len(tests) != 3 || tests[1] != "test -race ./internal/edit" ||
		tests[2] != "test -race -run "+goLanguage.run+" ./internal/go_language" {
		test.Fatalf("Kirk's Mac ran go tests %q, want the module's, then its -race run of ./internal/edit, then "+
			"./internal/go_language's with -run %s:\n%s", tests, goLanguage.run, recorded)
	}
}
