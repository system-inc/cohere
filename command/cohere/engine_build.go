package main

import (
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// A development cohere builds the Swift engine from source when its sources or the toolchain changed, and that
// build ran inside the run it was for, so the project's footer billed it as checking: a bare run at cohere's root
// printed `swift ✓ 💎 101s` while it compiled cohere-swift, and `0.9s` a minute later (#ket0hrp). The launcher's
// line about the build shows on a terminal alone, so a piped or logged run kept no trace of it. Now each build is
// recorded here, the project's time leaves it out, and the footer and `--json` say it on their own. A released
// cohere runs the engine it ships and builds nothing, so this is only ever set in development.

// swiftEngineName is the Swift engine's binary, as a build names it.
const swiftEngineName = "cohere-swift"

// engineBuild is one engine this invocation built from source before it could check.
type engineBuild struct {
	// Engine is the binary built, such as cohere-swift.
	Engine string
	// Key is the cache key it was built under, the hash its binary is named by.
	Key string
	// Elapsed is how long the build took.
	Elapsed time.Duration
}

// engineBuilds are this invocation's builds, in the order they ran.
var engineBuilds struct {
	lock   sync.Mutex
	builds []engineBuild
}

// recordEngineBuild notes a build of engine to binaryPath, which took elapsed. The key is what the binary's name
// ends in after its last dash, the cache key a hash-named binary carries.
func recordEngineBuild(engine string, binaryPath string, elapsed time.Duration) {
	name := filepath.Base(binaryPath)
	key := name[strings.LastIndex(name, "-")+1:]
	engineBuilds.lock.Lock()
	defer engineBuilds.lock.Unlock()
	engineBuilds.builds = append(engineBuilds.builds, engineBuild{Engine: engine, Key: key, Elapsed: elapsed})
}

// builtEngines is this invocation's builds, a copy.
func builtEngines() []engineBuild {
	engineBuilds.lock.Lock()
	defer engineBuilds.lock.Unlock()
	return append([]engineBuild(nil), engineBuilds.builds...)
}

// checkStart is when this invocation's check began: when the process did, plus whatever it spent building
// engines, so a project's time is the check's alone.
func checkStart() time.Time {
	start := processStart
	for _, build := range builtEngines() {
		start = start.Add(build.Elapsed)
	}
	return start
}

// segment is the build as the footer says it.
func (build engineBuild) segment() string {
	return "🔨 built " + build.Engine + " " + footerSeconds(build.Elapsed)
}

// engineBuildSegments are the footer's segments for every build, in order.
func engineBuildSegments(builds []engineBuild) []string {
	segments := make([]string, 0, len(builds))
	for _, build := range builds {
		segments = append(segments, build.segment())
	}
	return segments
}

// engineBuildJson is one build as a line of `--json`, before the summary it preceded.
type engineBuildJson struct {
	Kind    string  `json:"kind"`
	Engine  string  `json:"engine"`
	Key     string  `json:"key"`
	Seconds float64 `json:"seconds"`
}

// engineBuildLines are the builds as lines of `--json`.
func engineBuildLines(builds []engineBuild) string {
	var lines strings.Builder
	for _, build := range builds {
		lines.WriteString(jsonLine(engineBuildJson{
			Kind:    "engineBuild",
			Engine:  build.Engine,
			Key:     build.Key,
			Seconds: build.Elapsed.Seconds(),
		}))
	}
	return lines.String()
}
