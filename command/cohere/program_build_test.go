package main

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/system-inc/cohere/internal/needs"
	"github.com/system-inc/cohere/internal/release/dispatch"
	release "github.com/system-inc/cohere/internal/release/packaging"
)

// resetProgramBuilds empties this process's record of builds, and restores it when the test ends.
func resetProgramBuilds(test *testing.T) {
	test.Helper()
	programBuilds.lock.Lock()
	kept := programBuilds.builds
	programBuilds.builds = nil
	programBuilds.lock.Unlock()
	test.Cleanup(func() {
		programBuilds.lock.Lock()
		programBuilds.builds = kept
		programBuilds.lock.Unlock()
	})
}

// A development cohere that builds its cohere-swift records the build, with the child, the key its binary is
// named by and how long it took, and a Swift run is timed from after it, so the footer never bills a compile as
// the check (#ket0hrp: `swift ✓ 💎 101s`, then `0.9s` a minute later). One that finds the child built records
// nothing, and its run is timed from the process's start, as before.
// Not parallel: it swaps resolveDevelopmentSwiftBinary and swiftBinaryPlatform, and the package's record of
// builds, which every Swift resolve in this process reads.
func TestAProgramBuildIsRecordedAndLeftOutOfTheCheck(test *testing.T) {
	needs.Tool(test, "swift", "a Swift run asks the toolchain its version before it checks anything")
	keptResolve, keptPlatform := resolveDevelopmentSwiftBinary, swiftBinaryPlatform
	test.Cleanup(func() { resolveDevelopmentSwiftBinary, swiftBinaryPlatform = keptResolve, keptPlatform })
	swiftBinaryPlatform = release.Target{GoOperatingSystem: "darwin", GoArchitecture: "arm64"}
	test.Setenv(dispatch.SwiftBinaryOverrideVariable, "")
	test.Setenv(swiftBinaryPathVariable, "")
	const pause = 300 * time.Millisecond
	for _, resolve := range []struct {
		name  string
		built bool
	}{{name: "built", built: true}, {name: "already built"}} {
		// Not parallel: each subtest swaps the resolver the other reads.
		test.Run(resolve.name, func(test *testing.T) {
			resetProgramBuilds(test)
			resolveDevelopmentSwiftBinary = func(dispatch.Paths, string, int, string) (string, bool, error) {
				if resolve.built {
					time.Sleep(pause)
				}
				return "/cache/bin/cohere-swift-darwin-arm64-9ff2b6ea1bf8967f", resolve.built, nil
			}
			if _, _, resolveError := resolveSwiftBinary(
				projectLocation{},
				release.Provenance{Version: "dev"},
			); resolveError != nil {
				test.Fatal(resolveError)
			}
			builds := builtPrograms()
			run := swiftContractRun(bufio.NewWriter(os.Stdout), contractModeCheck, projectLocation{})
			offset := run.report.processStart.Sub(processStart)
			if !resolve.built {
				if len(builds) != 0 || offset != 0 {
					test.Errorf("a resolve that built nothing recorded %+v and moved the check's start by %s",
						builds, offset)
				}
				return
			}
			if len(builds) != 1 || builds[0].Program != "cohere-swift" || builds[0].Key != "9ff2b6ea1bf8967f" ||
				builds[0].Elapsed < pause {
				test.Fatalf("the build was recorded as %+v, want cohere-swift under 9ff2b6ea1bf8967f, %s or more",
					builds, pause)
			}
			if offset != builds[0].Elapsed {
				test.Errorf("the Swift run is timed from %s after the process began, want the build's %s", offset,
					builds[0].Elapsed)
			}
		})
	}
}

// A build is its own segment of the footer and its own `--json` record, and a run that built nothing says
// nothing new in either.
func TestAProgramBuildIsSaidApartFromTheCheck(test *testing.T) {
	test.Parallel()
	build := programBuild{Program: "cohere-swift", Key: "9ff2b6ea1bf8967f", Elapsed: 99 * time.Second}
	built := runSummary{Total: 900 * time.Millisecond, ProgramBuilds: []programBuild{build}}
	line := footer(built, textStyle{}, footerOptions{})
	if !strings.Contains(line, "✓ 💎 0.9s") || !strings.Contains(line, " • 🔨 built cohere-swift 99s") {
		test.Errorf("the footer of a run that built the child reads %q", line)
	}
	plain := footer(runSummary{Total: 900 * time.Millisecond}, textStyle{}, footerOptions{})
	if strings.Contains(plain, "🔨") {
		test.Errorf("the footer of a run that built nothing names a build: %q", plain)
	}
	facts := overallFacts{ProjectsByKind: map[string]int{"Swift": 2}, ProgramBuilds: []programBuild{build}}
	overall := overallFooter(facts, textStyle{})
	if !strings.Contains(overall, "🔨 built cohere-swift 99s") {
		test.Errorf("the overall footer of a run that built the child reads %q", overall)
	}

	var record programBuildJson
	if decodeError := json.Unmarshal([]byte(programBuildLines([]programBuild{build})), &record); decodeError != nil {
		test.Fatal(decodeError)
	}
	if record != (programBuildJson{
		Kind:    "programBuild",
		Program: "cohere-swift",
		Key:     "9ff2b6ea1bf8967f",
		Seconds: 99,
	}) {
		test.Errorf("the build's record is %+v", record)
	}
	if lines := programBuildLines(nil); lines != "" {
		test.Errorf("a run that built nothing writes %q", lines)
	}
}
