package main

import (
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// A development cohere builds cohere-swift from source when its sources or the toolchain changed, and that
// build ran inside the run it was for, so the project's footer billed it as checking: a bare run at cohere's root
// printed `swift ✓ 💎 101s` while it compiled cohere-swift, and `0.9s` a minute later (#ket0hrp). The launcher's
// line about the build shows on a terminal alone, so a piped or logged run kept no trace of it. Now each build is
// recorded here, the project's time leaves it out, and the footer and `--json` say it on their own. A released
// cohere runs the child it ships and builds nothing, so this is only ever set in development.

// swiftProgramName is cohere-swift's binary, as a build names it.
const swiftProgramName = "cohere-swift"

// programBuild is one child this invocation built from source before it could check.
type programBuild struct {
	// Program is the binary built, such as cohere-swift.
	Program string
	// Key is the cache key it was built under, the hash its binary is named by.
	Key string
	// Elapsed is how long the build took.
	Elapsed time.Duration
}

// programBuilds are this invocation's builds, in the order they ran.
var programBuilds struct {
	lock   sync.Mutex
	builds []programBuild
}

// recordProgramBuild notes a build of program to binaryPath, which took elapsed. The key is what the binary's name
// ends in after its last dash, the cache key a hash-named binary carries.
func recordProgramBuild(program string, binaryPath string, elapsed time.Duration) {
	name := filepath.Base(binaryPath)
	key := name[strings.LastIndex(name, "-")+1:]
	programBuilds.lock.Lock()
	defer programBuilds.lock.Unlock()
	programBuilds.builds = append(programBuilds.builds, programBuild{Program: program, Key: key, Elapsed: elapsed})
}

// builtPrograms is this invocation's builds, a copy.
func builtPrograms() []programBuild {
	programBuilds.lock.Lock()
	defer programBuilds.lock.Unlock()
	return append([]programBuild(nil), programBuilds.builds...)
}

// checkStart is when this invocation's check began: when the process did, plus whatever it spent building
// children, so a project's time is the check's alone.
func checkStart() time.Time {
	start := processStart
	for _, build := range builtPrograms() {
		start = start.Add(build.Elapsed)
	}
	return start
}

// segment is the build as the footer says it.
func (build programBuild) segment() string {
	return "🔨 built " + build.Program + " " + footerSeconds(build.Elapsed)
}

// programBuildSegments are the footer's segments for every build, in order.
func programBuildSegments(builds []programBuild) []string {
	segments := make([]string, 0, len(builds))
	for _, build := range builds {
		segments = append(segments, build.segment())
	}
	return segments
}

// programBuildJson is one build as a line of `--json`, before the summary it preceded.
type programBuildJson struct {
	Kind    string  `json:"kind"`
	Program string  `json:"program"`
	Key     string  `json:"key"`
	Seconds float64 `json:"seconds"`
}

// programBuildLines are the builds as lines of `--json`.
func programBuildLines(builds []programBuild) string {
	var lines strings.Builder
	for _, build := range builds {
		lines.WriteString(jsonLine(programBuildJson{
			Kind:    "programBuild",
			Program: build.Program,
			Key:     build.Key,
			Seconds: build.Elapsed.Seconds(),
		}))
	}
	return lines.String()
}
