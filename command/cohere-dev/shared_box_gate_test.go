//go:build unix

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/executable_testing"
	"github.com/system-inc/cohere/internal/needs"
)

// plantedGo stands in for a shared box's go: it answers go env, and records every go mod verify and go test it is
// asked for in GATE_LOG, with the Go settings it ran under and whether its Go cache was empty, so a test can read what
// the gate asked of it. GATE_VERIFY_FAILS makes go mod verify fail as a tampered module cache would.
const plantedGo = `#!/bin/sh
case "$1" in
env) [ "$2" = GOOS ] && echo linux; [ "$2" = GOARCH ] && echo amd64; exit 0 ;;
mod)
  echo "verify" >> "$GATE_LOG"
  if [ -n "$GATE_VERIFY_FAILS" ]; then echo "example.com/module v1.0.0: dir has been modified"; exit 1; fi
  echo "all modules verified" ;;
test)
  empty=no; [ -z "$(ls -A "$GOCACHE")" ] && empty=yes
  echo "test $* | cache $GOCACHE empty $empty | toolchain $GOTOOLCHAIN | goenv $GOENV | goflags $GOFLAGS | path $PATH" >> "$GATE_LOG"
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

// A shared box's gate trusts nothing the box kept (#adbkt5t). Its toolchain must digest to the pinned one before any
// of it runs, its module cache must pass go mod verify before any test, and every test runs with -count=1 on a Go
// cache made empty for the job, with the account's Go settings left unread, and the cache is gone after. Shown by
// running the gate's own script with bash against a planted toolchain whose go records what it is asked.
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
	}{
		{name: "a toolchain as pinned runs the tests", node: "pinned"},
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
			log := filepath.Join(test.TempDir(), "asked")
			temporary := test.TempDir()
			// The account's own Go settings, which the gate must not read: a cache, flags and a toolchain switch.
			environment := append(os.Environ(), "GATE_LOG="+log, "TMPDIR="+temporary, "GOCACHE="+test.TempDir(),
				"GOFLAGS=-count=5", "GOTOOLCHAIN=auto", "GOENV="+filepath.Join(test.TempDir(), "env"))
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
			gate := exec.Command("bash", "-c", script, "gate", goroot, digest, node, nodeDigest, "./internal/edit",
				"./...")
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
			if gateError != nil {
				test.Fatalf("the gate failed: %v\n%s", gateError, output)
			}
			lines := strings.Split(strings.TrimSpace(string(asked)), "\n")
			if len(lines) != 3 || lines[0] != "verify" {
				test.Fatalf("the gate asked for %q, want go mod verify, then the module's tests and the -race run",
					lines)
			}
			for _, line := range lines[1:] {
				for _, want := range []string{"-count=1", "empty yes", "toolchain local", "goenv off",
					"goflags -buildvcs=false -trimpath", "cache " + temporary} {
					if !strings.Contains(line, want) {
						test.Errorf("a go test the gate asked for lacks %q: %s", want, line)
					}
				}
			}
			if !strings.Contains(lines[2], "-race ./internal/edit") {
				test.Errorf("the -race run is not of the race list: %s", lines[2])
			}
			// A pinned node is on the tests' path, before the system's own; an absent one is nowhere on it.
			onPath := strings.Contains(lines[1], "path "+goroot+"/bin:"+node+"/bin:/usr/bin:/bin")
			if offPath := strings.Contains(lines[1], "path "+goroot+"/bin:/usr/bin:/bin"); (row.node == "pinned" &&
				!onPath) || (row.node == "absent" && !offPath) {
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

// The Mac's runs keep Go's test cache, which is keyed honestly there: the exception is for shared boxes alone, so no
// go test the Mac's half of a landing runs carries -count. Shown through landings whose go records its arguments:
// one tested on the Mac alone, module and -race run, and one Home passed with a skip, whose remainder the Mac runs.
func TestTheMacsGateRunsKeepTheTestCache(test *testing.T) {
	test.Parallel()
	for _, home := range []string{"off", "a stand-in"} {
		test.Run("Home "+home, func(test *testing.T) {
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
			fixture.git(worktree, "add", ".")
			fixture.git(worktree, "commit", "-q", "-m", fixtureSubject("a module"))
			fixture.git(worktree, "merge", "-q", "--no-edit", "-m", "Merge main", "main")
			setting := "off"
			if home != "off" {
				setting = filepath.Join(test.TempDir(), "home")
				reply := "#!/bin/sh\necho \"" + homeMarker + "$1 on linux/amd64\"\n" +
					`echo '{"Action":"pass","Package":"example.com/fixture/internal/edit"}'` + "\n" +
					`echo '{"Action":"skip","Package":"example.com/fixture/internal/edit","Test":"TestEdit"}'` + "\n" +
					"echo \"" + homeRaceMarker + "\"\n" +
					`echo '{"Action":"pass","Package":"example.com/fixture/internal/edit"}'` + "\n"
				if writeError := executable_testing.WriteFile(setting, []byte(reply), 0o755); writeError != nil {
					test.Fatal(writeError)
				}
			}
			landing := exec.Command(fixture.wrapper, "land", "--message", "a module tested")
			landing.Dir, landing.Env = worktree, append(fixture.environment, homeGateVariable+"="+setting)
			output, landError := landing.CombinedOutput()
			if landError != nil {
				test.Fatalf("the landing failed: %v\n%s", landError, output)
			}
			recorded, readError := os.ReadFile(asked)
			if readError != nil {
				test.Fatal(readError)
			}
			tests := 0
			for _, line := range strings.Split(strings.TrimSpace(string(recorded)), "\n") {
				if !strings.HasPrefix(line, "test") {
					continue
				}
				tests++
				if strings.Contains(line, "-count") {
					test.Errorf("the Mac ran a go test with -count, which throws its honest cache away: go %s", line)
				}
			}
			if tests == 0 {
				test.Fatalf("the Mac ran no go test, so nothing was checked:\n%s", recorded)
			}
		})
	}
}
