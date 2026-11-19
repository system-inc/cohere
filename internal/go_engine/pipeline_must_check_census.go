package go_engine

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/system-inc/cohere/internal/go_engine/build_dependence"
	"github.com/system-inc/cohere/internal/go_engine/project"
)

// The must-check census (#anfjtr3, stage 6's first unit): each extra build's must-check set, the packages the build
// must check for what it does to their declarations, written so the set a plain run decides from block 1's bits can be
// held to the one `--build-dependence`'s whole graph decides for the same tree. It decides nothing: nothing reads the
// set but the census. Under `COHERE_MUST_CHECK_CENSUS=<directory>` each extra build writes two files there, named for
// the build (buildFileStem): `<build>.txt`, its set, one package a line as `<ID>\t<reasons>`, sorted by ID, and
// `<build>.wide.txt`, what its judgment answered wide for want of any declarations, empty when nothing was, beside
// `<build>.judgment.txt`, where and how long it was judged (judgmentStamp). A package
// is in the set for any of three reasons, written in this order, comma-separated (mustCheckReasons):
//   - Files: both builds have it and its files differ: a file one compiles and the other does not, a line directive
//     in one they do not share, or a cgo tool on either side (filesDiffer);
//   - BuildOnly: only the build has it;
//   - Declares: a unit of it declares an object the build makes dependent (build_dependence.Dependence's
//     DependentPackages).
//
// A package only the host has is in no build's set: the build has nothing of it to check, though every unit of it is
// dependent there and it may declare a dependent object. The wide file names, a line each as `<ID>\tNoGraph`, every
// package the host restored that the judgment holds in no graph, whose objects are each answered dependent on both
// bits wherever a unit reads them, and then, as `WithoutCounterpart\t<count>`, the objects of a package a graph does
// hold that it answered wide for want of a counterpart there, when any was. A run without the variable judges and
// writes nothing more than it did.

// mustCheckCensusVariable names the directory each extra build's must-check set is written into.
const mustCheckCensusVariable = "COHERE_MUST_CHECK_CENSUS"

// mustCheckReason is why a package is in a build's must-check set.
type mustCheckReason string

const (
	mustCheckFiles     mustCheckReason = "Files"
	mustCheckBuildOnly mustCheckReason = "BuildOnly"
	mustCheckDeclares  mustCheckReason = "Declares"
)

// mustCheckReasons are the reasons in the order a line writes them.
var mustCheckReasons = []mustCheckReason{mustCheckFiles, mustCheckBuildOnly, mustCheckDeclares}

// mustCheckSet is one build's must-check census, each as its file writes it: lines its set's, and wide what its
// judgment answered wide. The judgment's stamp is written apart (judgmentStamp): at, where the build was judged,
// judged how long the judgment took and waited how long it waited for its graph first.
type mustCheckSet struct {
	lines  []string
	wide   []string
	at     mustCheckJudgedAt
	judged time.Duration
	waited time.Duration
}

// mustCheckJudgedAt is where a build's must-check set was judged: at its listing, before its restore, as a plain run
// judges it, or after its load, as `--build-dependence` does.
type mustCheckJudgedAt string

const (
	mustCheckAtListing mustCheckJudgedAt = "Listing"
	mustCheckAtLoad    mustCheckJudgedAt = "Load"
)

// judgmentStamp is the third file a build writes (`<build>.judgment.txt`): where it was judged, and the judgment's
// milliseconds and its wait for its graph's, a name and a value a line, for a pair to price the judgment by. Its
// numbers differ run to run, so a census compared file for file leaves it out.
func judgmentStamp(set *mustCheckSet) []string {
	return []string{"at\t" + string(set.at), "milliseconds\t" + strconv.FormatInt(set.judged.Milliseconds(), 10),
		"waited\t" + strconv.FormatInt(set.waited.Milliseconds(), 10)}
}

// mustCheckLines are the set's lines of the build differences describe, judged by judgments, each a graph's judgment
// of its own difference, nil for a graph that judged nothing: every package a difference holds, by whether the build
// alone has it, and every package a judgment's dependent objects are declared in, less every package a difference
// says only the host has.
func mustCheckLines(differences []build_dependence.Difference, judgments []*build_dependence.Dependence) []string {
	hostOnly := map[string]bool{}
	for _, difference := range differences {
		maps.Copy(hostOnly, difference.HostOnlyPackages)
	}
	reasons := map[string]map[mustCheckReason]bool{}
	add := func(identifier string, reason mustCheckReason) {
		if hostOnly[identifier] {
			return
		}
		if reasons[identifier] == nil {
			reasons[identifier] = map[mustCheckReason]bool{}
		}
		reasons[identifier][reason] = true
	}
	for _, difference := range differences {
		for identifier := range difference.Packages {
			if difference.BuildOnlyPackages[identifier] {
				add(identifier, mustCheckBuildOnly)
				continue
			}
			add(identifier, mustCheckFiles)
		}
	}
	for _, judged := range judgments {
		if judged == nil {
			continue
		}
		for identifier := range judged.DependentPackages() {
			add(identifier, mustCheckDeclares)
		}
	}
	lines := make([]string, 0, len(reasons))
	for _, identifier := range slices.Sorted(maps.Keys(reasons)) {
		var written []string
		for _, reason := range mustCheckReasons {
			if reasons[identifier][reason] {
				written = append(written, string(reason))
			}
		}
		lines = append(lines, identifier+"\t"+strings.Join(written, ","))
	}
	return lines
}

// mustCheckWide are the wide file's lines: each of notHeld, a package the judgment holds in no graph, sorted, then the
// count of the objects answered wide for want of a counterpart, when there were any.
func mustCheckWide(notHeld []string, withoutCounterpart int) []string {
	lines := make([]string, 0, len(notHeld)+1)
	for _, identifier := range slices.Sorted(slices.Values(notHeld)) {
		lines = append(lines, identifier+"\tNoGraph")
	}
	if withoutCounterpart > 0 {
		lines = append(lines, "WithoutCounterpart\t"+strconv.Itoa(withoutCounterpart))
	}
	return lines
}

// buildFileStem is the name a file of build's own is given in a census's directory, its build's name with no slash or
// space, before its suffix.
func buildFileStem(build project.Build) string {
	return strings.NewReplacer("/", "_", " ", "_").Replace(build.String())
}

// writeMustCheckCensus writes each extra build's must-check set of runs, and its wide file, into the census's
// directory, when the run was asked to and kept the build's set. A file that cannot be written is noted and changes
// nothing else.
func (engine *pipeline) writeMustCheckCensus(runs []buildRun) {
	if engine.mustCheckCensus == "" {
		return
	}
	for _, run := range runs[1:] {
		if run.mustCheck == nil {
			continue
		}
		stem := filepath.Join(engine.mustCheckCensus, buildFileStem(run.build))
		for _, file := range []struct {
			name  string
			lines []string
		}{{stem + ".txt", run.mustCheck.lines}, {stem + ".wide.txt", run.mustCheck.wide},
			{stem + ".judgment.txt", judgmentStamp(run.mustCheck)}} {
			contents := ""
			if len(file.lines) > 0 {
				contents = strings.Join(file.lines, "\n") + "\n"
			}
			if writeError := os.WriteFile(file.name, []byte(contents), 0o644); writeError != nil {
				engine.note("the must-check census: %v", writeError)
			}
		}
	}
}
