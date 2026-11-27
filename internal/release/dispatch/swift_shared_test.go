package dispatch

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/release/packaging"
	"github.com/system-inc/cohere/internal/testing/executable_testing"
)

// A job whose cache is emptied every run takes cohere-swift from a shared folder, once the binary's bytes match the
// sum beside it, and leaves each binary it builds in its own layer for that folder (#e94a6r4). These tests resolve
// both ways a binary is named, from the working tree and from a commit, with the fixture's stand-in for SwiftPM,
// which records each build it runs in fixture.stated, and read what each resolve took and left where.

// swiftResolveKinds are the two ways cohere-swift is resolved by a hash, by whether the resolve names a commit.
var swiftResolveKinds = map[string]bool{"from the working tree": false, "from a commit": true}

// sharedSwiftScript is a binary no build here writes: it says it was shared, and reports the clean tree a committed
// binary's proof asks for.
const sharedSwiftScript = "#!/bin/sh\nif [ \"$3\" = \"--version\" ]; then\n" +
	"  echo '{\"kind\":\"provenance\",\"sourceTreeModified\":false}'\n  exit 0\nfi\necho 'rule=shared'\n"

// Not parallel: newSwiftFixture swaps the package variable buildSwiftProduct for its own stand-in, and this names the
// shared folder with test.Setenv.
func TestASharedSwiftBinaryItsSumVouchesForIsTakenAndNothingIsBuilt(test *testing.T) {
	for kind, committed := range swiftResolveKinds {
		// Not parallel: each swaps buildSwiftProduct (newSwiftFixture) and sets a folder with test.Setenv.
		test.Run(kind, func(test *testing.T) {
			fixture := newSwiftFixture(test)
			said := captureReport(test)
			shared := test.TempDir()
			test.Setenv(SwiftEngineSharedVariable, shared)
			entry := filepath.Join(shared, filepath.Base(fixture.swiftBinaryPath(test, committed)))
			writeSharedSwiftBinary(test, entry, sharedSwiftScript, sumOf(sharedSwiftScript)+"\n")

			swiftBinary, built := fixture.resolve(test, committed)
			if built || len(fixture.stated) != 0 {
				test.Fatalf("a shared binary its sum vouches for was built anyway (built=%v, builds=%d)", built,
					len(fixture.stated))
			}
			if swiftBinary != fixture.swiftBinaryPath(test, committed) {
				test.Errorf("the shared binary was served from %s, not the cache", swiftBinary)
			}
			if output := run(test, swiftBinary); output != "rule=shared\n" {
				test.Errorf("the binary served printed %q, so it is not the shared one", output)
			}
			if committed {
				if _, statError := os.Stat(fixture.paths.SwiftSourceWorktree()); !errors.Is(statError, os.ErrNotExist) {
					test.Errorf("a committed binary taken from the shared folder checked out a worktree: %v", statError)
				}
			}
			if said.Len() != 0 {
				test.Errorf("a shared binary taken whole was said on stderr: %q", said.String())
			}
		})
	}
}

// Not parallel: newSwiftFixture swaps the package variable buildSwiftProduct for its own stand-in, and this names the
// shared folder with test.Setenv.
func TestASharedSwiftBinaryItsSumDoesNotVouchForIsRefusedAndBuilt(test *testing.T) {
	cases := map[string]struct {
		// sum is what the sum file holds, or empty for no sum file.
		sum string
		// says is what the one line on stderr holds besides the entry: the copy's sum beside the one the sum file
		// holds, or why there was none to compare.
		says []string
	}{
		"a sum of other bytes": {
			sum:  sumOf("other bytes") + "\n",
			says: []string{sumOf(sharedSwiftScript), sumOf("other bytes")},
		},
		"no sum file":           {says: []string{swiftEngineSumSuffix, "could not be read"}},
		"a sum that is not one": {sum: "not a sum\n", says: []string{sumOf(sharedSwiftScript), `"not a sum"`}},
	}
	for kind, committed := range swiftResolveKinds {
		for name, refused := range cases {
			// Not parallel: each swaps buildSwiftProduct (newSwiftFixture) and sets a folder with test.Setenv.
			test.Run(kind+", "+name, func(test *testing.T) {
				fixture := newSwiftFixture(test)
				said := captureReport(test)
				shared := test.TempDir()
				test.Setenv(SwiftEngineSharedVariable, shared)
				entry := filepath.Join(shared, filepath.Base(fixture.swiftBinaryPath(test, committed)))
				writeSharedSwiftBinary(test, entry, sharedSwiftScript, refused.sum)

				swiftBinary, built := fixture.resolve(test, committed)
				if !built || len(fixture.stated) != 1 {
					test.Fatalf("a shared binary its sum does not vouch for was not built over (built=%v, builds=%d)",
						built, len(fixture.stated))
				}
				// The build installs without replacing a file already there, so this also reads that a refused copy
				// was removed before it.
				if output := run(test, swiftBinary); output != "rule=one\n" {
					test.Errorf("the binary served printed %q, so the refused one was used", output)
				}
				expectNoPartialFiles(test, fixture.paths.BinaryDirectory())
				line := said.String()
				for _, expected := range append([]string{entry}, refused.says...) {
					if !strings.Contains(line, expected) {
						test.Errorf("the refusal said %q, which does not name %q", line, expected)
					}
				}
				if strings.Count(line, "\n") != 1 {
					test.Errorf("the refusal was not one line: %q", line)
				}
				if contents := readFile(test, entry); contents != sharedSwiftScript {
					test.Errorf("the shared folder's binary was changed to %q", contents)
				}
			})
		}
	}
}

// A binary built here is left in the layer under its own name beside its sum, and a later job reading that layer as
// its shared folder takes it without building. The later job's checkout is at another path, as Loom's jobs are, and
// its commit has another id, and it still finds the binary: neither is in the key.
//
// Not parallel: newSwiftFixture swaps the package variable buildSwiftProduct for its own stand-in, and this names the
// folders with test.Setenv.
func TestABuiltSwiftBinaryIsLeftInTheLayerBesideItsSum(test *testing.T) {
	for kind, committed := range swiftResolveKinds {
		// Not parallel: each swaps buildSwiftProduct (newSwiftFixture) and sets a folder with test.Setenv.
		test.Run(kind, func(test *testing.T) {
			fixture := newSwiftFixture(test)
			layer := filepath.Join(test.TempDir(), "layer")
			test.Setenv(SwiftEngineSharedVariable, test.TempDir())
			test.Setenv(SwiftEngineLayerVariable, layer)

			swiftBinary, built := fixture.resolve(test, committed)
			if !built {
				test.Fatal("an empty cache and an empty shared folder reported no build")
			}
			entry := filepath.Join(layer, filepath.Base(swiftBinary))
			contents := readFile(test, swiftBinary)
			if readFile(test, entry) != contents {
				test.Errorf("the layer's %s is not the binary built", entry)
			}
			if sum := readFile(test, entry+swiftEngineSumSuffix); sum != sumOf(contents)+"\n" {
				test.Errorf("the layer's sum reads %q, and the binary's sha256 is %s", sum, sumOf(contents))
			}
			info, statError := os.Stat(entry + swiftEngineSumSuffix)
			if statError != nil || info.Mode().Perm() != 0o644 {
				test.Errorf("the layer's sum is not readable by whoever reads the shared folder: %v, %v", info,
					statError)
			}
			entries, readDirectoryError := os.ReadDir(layer)
			if readDirectoryError != nil {
				test.Fatal(readDirectoryError)
			}
			if len(entries) != 2 {
				test.Errorf("the layer holds %d entries, want the binary and its sum", len(entries))
			}
			expectNoPartialFiles(test, layer)

			later := newSwiftFixture(test)
			test.Setenv(SwiftEngineSharedVariable, layer)
			taken, built := later.resolve(test, committed)
			if built || len(later.stated) != 0 {
				test.Fatalf("a later job built the binary its shared folder holds (built=%v)", built)
			}
			if readFile(test, taken) != contents {
				test.Error("a later job was served other bytes than the layer's")
			}
		})
	}
}

// Without a layer a build is left in the cache alone, and a shared folder is only ever read. With neither variable
// set, resolving is what it was before either existed, which every other Swift test here also runs under.
//
// Not parallel: newSwiftFixture swaps the package variable buildSwiftProduct for its own stand-in, and this names the
// shared folder with test.Setenv.
func TestABuildWithNoLayerWritesNothingOutsideTheCache(test *testing.T) {
	for kind, committed := range swiftResolveKinds {
		for named, namesShared := range map[string]bool{"nothing named": false, "a shared folder named": true} {
			// Not parallel: each swaps buildSwiftProduct (newSwiftFixture) and sets a folder with test.Setenv.
			test.Run(kind+", "+named, func(test *testing.T) {
				fixture := newSwiftFixture(test)
				shared := test.TempDir()
				if namesShared {
					test.Setenv(SwiftEngineSharedVariable, shared)
				}

				swiftBinary, built := fixture.resolve(test, committed)
				if !built || filepath.Dir(swiftBinary) != fixture.paths.BinaryDirectory() {
					test.Fatalf("a miss was not built into the cache (built=%v, at %s)", built, swiftBinary)
				}
				if entries := directoryNames(test, shared); len(entries) != 0 {
					test.Errorf("the shared folder was written: %v", entries)
				}
				if entries := directoryNames(test, filepath.Dir(fixture.repository)); !slices.Equal(
					entries,
					[]string{"cohere"},
				) {
					test.Errorf("the fixture's root holds %v, want only its repository", entries)
				}
			})
		}
	}
}

// A folder named by a relative path would be read against whatever directory cohere runs in, so it is refused.
//
// Not parallel: newSwiftFixture swaps the package variable buildSwiftProduct for its own stand-in, and this names the
// folders with test.Setenv.
func TestASwiftFolderNamedByARelativePathIsRefused(test *testing.T) {
	for _, variable := range []string{SwiftEngineSharedVariable, SwiftEngineLayerVariable} {
		// Not parallel: each swaps buildSwiftProduct (newSwiftFixture) and sets a folder with test.Setenv.
		test.Run(variable, func(test *testing.T) {
			fixture := newSwiftFixture(test)
			test.Setenv(variable, "relative/folder")
			_, _, resolveSwiftBinaryError := ResolveSwiftBinary(fixture.paths, "", 2, testSwiftVersionOutput)
			if resolveSwiftBinaryError == nil || !strings.Contains(resolveSwiftBinaryError.Error(), variable) {
				test.Fatalf("a relative %s was not refused by name: %v", variable, resolveSwiftBinaryError)
			}
		})
	}
}

// The key a binary is shared under changes with each thing that makes its bytes, alone: the toolchain, the build's
// configuration and its sources, and for a working-tree binary the tree's state. The platform is in the key too, and
// in the name SwiftBinaryPath gives it beside the key, but the runtime's own constants can't be changed here.
//
// Not parallel: newSwiftFixture swaps the package variable buildSwiftProduct for its own stand-in, and this swaps
// swiftBuildConfiguration.
func TestTheSwiftBinaryKeyChangesWithEachInputAlone(test *testing.T) {
	fixture := newSwiftFixture(test)
	toolchain, swiftToolchainError := swiftToolchain(testSwiftVersionOutput)
	if swiftToolchainError != nil {
		test.Fatal(swiftToolchainError)
	}
	packageDirectory := fixture.paths.SwiftPackageDirectory()
	development := func(toolchain string, modified bool) string {
		test.Helper()
		hash, swiftPackageHashError := swiftPackageHash(packageDirectory, toolchain, modified)
		if swiftPackageHashError != nil {
			test.Fatal(swiftPackageHashError)
		}
		return hash
	}
	committed := func(toolchain string) string {
		test.Helper()
		hash, committedSwiftPackageHashError := committedSwiftPackageHash(fixture.paths, fixture.head(test), toolchain)
		if committedSwiftPackageHashError != nil {
			test.Fatal(committedSwiftPackageHashError)
		}
		return hash
	}
	withConfiguration := func(read func() string) string {
		test.Helper()
		original := swiftBuildConfiguration
		swiftBuildConfiguration = []string{"-c", "debug", "--manifest-cache", "none", "--product", "cohere-swift"}
		defer func() { swiftBuildConfiguration = original }()
		return read()
	}

	keys := map[string]string{
		"working tree":                    development(toolchain, false),
		"working tree, another toolchain": development(toolchain+" (patched)", false),
		"working tree, modified":          development(toolchain, true),
		"commit":                          committed(toolchain),
		"commit, another toolchain":       committed(toolchain + " (patched)"),
	}
	keys["working tree, another configuration"] = withConfiguration(func() string {
		return development(toolchain, false)
	})
	keys["commit, another configuration"] = withConfiguration(func() string { return committed(toolchain) })
	if again := development(toolchain, false); again != keys["working tree"] {
		test.Fatalf("the same working tree was keyed %s, then %s", keys["working tree"], again)
	}
	writeFile(test, fixture.rule(), "two\n")
	keys["working tree, another source"] = development(toolchain, false)
	fixture.commit(test, "swift_language")
	keys["commit, another source"] = committed(toolchain)

	named := map[string]string{}
	for input, key := range keys {
		if earlier, found := named[key]; found {
			test.Errorf("%s and %s were given the same key %s", earlier, input, key)
		}
		named[key] = input
	}
}

// The build passes the configuration the hashes name, so the two can't drift apart.
//
// Not parallel: it puts a fake swift first on PATH with test.Setenv.
func TestTheSwiftBuildIsHandedTheConfigurationItsKeyNames(test *testing.T) {
	bin := test.TempDir()
	product := test.TempDir()
	recorded := filepath.Join(test.TempDir(), "arguments")
	writeFile(test, filepath.Join(product, "cohere-swift"), "#!/bin/sh\n")
	fake := "#!/bin/sh\ncase \"$*\" in\n*--show-bin-path*) echo " + product + " ;;\n" +
		"*) printf ' %s ' \"$*\" > " + recorded + " ;;\nesac\n"
	if writeError := executable_testing.WriteFile(filepath.Join(bin, "swift"), []byte(fake), 0o755); writeError != nil {
		test.Fatal(writeError)
	}
	test.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	test.Setenv(release.SwiftResolvedVariable, "")

	if buildError := buildSwiftProductWithSwiftPm(
		test.TempDir(),
		test.TempDir(),
		filepath.Join(test.TempDir(), "bin", "cohere-swift"),
		nil,
	); buildError != nil {
		test.Fatal(buildError)
	}
	if arguments := readFile(test, recorded); !strings.Contains(arguments, " "+swiftBuildIdentity()+" ") {
		test.Errorf("swift was run with %q, which does not hold the configuration the key names, %q", arguments,
			swiftBuildIdentity())
	}
}

// swiftBinaryPath is where the fixture's cohere-swift is cached, as resolve names it.
func (fixture *swiftFixture) swiftBinaryPath(test *testing.T, committed bool) string {
	test.Helper()
	toolchain, swiftToolchainError := swiftToolchain(testSwiftVersionOutput)
	if swiftToolchainError != nil {
		test.Fatal(swiftToolchainError)
	}
	if committed {
		hash, committedSwiftPackageHashError := committedSwiftPackageHash(
			fixture.paths,
			fixture.head(test),
			testToolchain,
		)
		if committedSwiftPackageHashError != nil {
			test.Fatal(committedSwiftPackageHashError)
		}
		return fixture.paths.SwiftBinaryPath(hash)
	}
	hash, swiftPackageHashError := swiftPackageHash(
		fixture.paths.SwiftPackageDirectory(),
		toolchain,
		SwiftPackageTreeModified(fixture.paths.ModuleDirectory),
	)
	if swiftPackageHashError != nil {
		test.Fatal(swiftPackageHashError)
	}
	return fixture.paths.SwiftBinaryPath(hash)
}

// resolve resolves the fixture's cohere-swift from its working tree, or from its head when committed, failing the
// test on error.
func (fixture *swiftFixture) resolve(test *testing.T, committed bool) (string, bool) {
	test.Helper()
	if !committed {
		return fixture.resolveDevelopment(test)
	}
	swiftBinary, built, resolveCommittedSwiftBinaryError := resolveCommittedSwiftBinary(
		fixture.paths,
		fixture.head(test),
		testToolchain,
		2,
	)
	if resolveCommittedSwiftBinaryError != nil {
		test.Fatalf("resolving the committed binary: %v", resolveCommittedSwiftBinaryError)
	}
	return swiftBinary, built
}

// captureReport points Report at a buffer for the test, and gives the buffer back.
func captureReport(test *testing.T) *bytes.Buffer {
	test.Helper()
	var said bytes.Buffer
	previous := Report
	Report = NewProgress(&said, false, false)
	test.Cleanup(func() { Report = previous })
	return &said
}

// writeSharedSwiftBinary writes a shared folder's binary at entry, and its sum file holding sum unless sum is empty.
func writeSharedSwiftBinary(test *testing.T, entry string, contents string, sum string) {
	test.Helper()
	if writeError := executable_testing.WriteFile(entry, []byte(contents), 0o755); writeError != nil {
		test.Fatal(writeError)
	}
	if sum != "" {
		writeFile(test, entry+swiftEngineSumSuffix, sum)
	}
}

// sumOf is the sha256 of contents in lowercase hex, as a sum file spells it.
func sumOf(contents string) string {
	sum := sha256.Sum256([]byte(contents))
	return hex.EncodeToString(sum[:])
}

// directoryNames are the names directory holds, sorted.
func directoryNames(test *testing.T, directory string) []string {
	test.Helper()
	entries, readDirectoryError := os.ReadDir(directory)
	if readDirectoryError != nil {
		test.Fatal(readDirectoryError)
	}
	names := []string{}
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}
