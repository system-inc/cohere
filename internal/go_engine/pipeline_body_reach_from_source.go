package go_engine

import (
	"os"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/project"
)

// The body reach census's base, the packages each build checks from source today (#w72zxtq): under
// `--build-dependence` a build restores nothing from the host's run, so it checks every package from source, and the
// reach is read beside today's verdict only over those a plain run checks. A plain run records them under
// `COHERE_BODY_REACH_FROM_SOURCE_RECORD=<directory>`, a file per extra build, and the census reads them back under
// `COHERE_BODY_REACH_FROM_SOURCE=<directory>`, counting every number again for that subset beside the whole.

// bodyReachFromSourceRecordVariable names the directory a run records each extra build's from-source packages into.
const bodyReachFromSourceRecordVariable = "COHERE_BODY_REACH_FROM_SOURCE_RECORD"

// bodyReachFromSourceVariable names the directory the census reads each build's recorded from-source packages from.
const bodyReachFromSourceVariable = "COHERE_BODY_REACH_FROM_SOURCE"

// fromSourceIdentifiers are the IDs of the packages of loaded's whole graph it checked from source, sorted, as
// checkedFromSource counts them.
func fromSourceIdentifiers(loaded *load.Loaded) []string {
	var identifiers []string
	packages.Visit(loaded.Roots, nil, func(each *packages.Package) {
		if each.TypesInfo != nil {
			identifiers = append(identifiers, each.ID)
		}
	})
	slices.Sort(identifiers)
	return identifiers
}

// fromSourceRecordFile is the file of directory build's from-source packages are recorded in, named for the build.
func fromSourceRecordFile(directory string, build project.Build) string {
	return filepath.Join(directory, buildFileStem(build)+".txt")
}

// writeFromSourceRecord writes each extra build's from-source packages of runs into the record's directory, one ID a
// line, when the run was asked to record them. A file that cannot be written is noted and changes nothing else.
func (engine *pipeline) writeFromSourceRecord(runs []buildRun) {
	if engine.bodyReachFromSourceRecord == "" {
		return
	}
	for _, run := range runs[1:] {
		contents := strings.Join(run.fromSourceIdentifiers, "\n") + "\n"
		file := fromSourceRecordFile(engine.bodyReachFromSourceRecord, run.build)
		if writeError := os.WriteFile(file, []byte(contents), 0o644); writeError != nil {
			engine.note("the body reach census's from-source record: %v", writeError)
		}
	}
}

// recordedFromSource are build's from-source packages as a plain run recorded them, by ID, nil when the census was
// given no record or holds none of build, which the census then counts over the whole alone.
func (engine *pipeline) recordedFromSource(build project.Build) map[string]bool {
	if engine.bodyReachFromSource == "" {
		return nil
	}
	contents, readError := os.ReadFile(fromSourceRecordFile(engine.bodyReachFromSource, build))
	if readError != nil {
		return nil
	}
	recorded := map[string]bool{}
	for line := range strings.Lines(string(contents)) {
		if identifier := strings.TrimSpace(line); identifier != "" {
			recorded[identifier] = true
		}
	}
	return recorded
}
