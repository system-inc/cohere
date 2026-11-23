package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"time"

	"github.com/system-inc/cohere/internal/variational_types/types"
)

// standInImporter stands in for internal/variational_types/merged until that lands. It reads no export data: each build
// checks a package from its own listed source with the fork, plainly, and Import answers with the first build that
// lists the package, merging nothing. So a package that differs by build answers as that one build has it, and the
// projection to every other build can mismatch where the merged importer's wouldn't. Its decode time is the time the
// source checks took.
type standInImporter struct {
	fileSet *token.FileSet
	builds  []*listedBuild
	sizes   types.Sizes
	added   map[string]*types.Package
	plain   []*plainSourceImporter
	decode  time.Duration
	// depth is how many source checks are running, so a check nested in another is timed once.
	depth int
	// errors counts the errors the source checks reported, and firstErrors spells a few.
	errors      int
	firstErrors []string
}

// importerName names the importer the run uses, for the report.
const importerName = "stand-in: each build's packages checked from source, the first build listing a package " +
	"answering for every build, merging nothing"

// newImporter is the importer the variational check and the baselines import through. It is the stand-in until
// internal/variational_types/merged lands, when this becomes merged.New(fileSet, exports, all).
func newImporter(
	fileSet *token.FileSet,
	exports []map[string]string,
	all types.Mask,
	builds []*listedBuild,
	sizes types.Sizes,
) mergingImporter {
	return newStandInImporter(fileSet, builds, sizes)
}

// noteImporterErrors reports the stand-in's own source checks' errors.
//
// Mutates: report gets the errors.
func (report *spikeReport) noteImporterErrors(importer mergingImporter) {
	if standIn, isStandIn := importer.(*standInImporter); isStandIn {
		report.Errors.StandIn = standIn.errors
		report.Errors.StandInFirst = standIn.firstErrors
	}
}

// plainSourceImporter is one build's packages, each checked from that build's listed source.
type plainSourceImporter struct {
	standIn *standInImporter
	build   int
	checked map[string]*types.Package
}

// newStandInImporter is the stand-in for builds, parsing into fileSet.
func newStandInImporter(fileSet *token.FileSet, builds []*listedBuild, sizes types.Sizes) *standInImporter {
	standIn := &standInImporter{fileSet: fileSet, builds: builds, sizes: sizes, added: map[string]*types.Package{}}
	for index := range builds {
		standIn.plain = append(standIn.plain,
			&plainSourceImporter{standIn: standIn, build: index, checked: map[string]*types.Package{}})
	}
	return standIn
}

func (standIn *standInImporter) Import(path string) (*types.Package, error) {
	if checked, isAdded := standIn.added[path]; isAdded {
		return checked, nil
	}
	for index, build := range standIn.builds {
		if _, listed := build.packages[path]; listed {
			return standIn.plain[index].Import(path)
		}
	}
	return nil, fmt.Errorf("stand-in importer: no build lists %s", path)
}

func (standIn *standInImporter) Add(checked *types.Package) { standIn.added[checked.Path()] = checked }

func (standIn *standInImporter) Plain(build int) types.Importer { return standIn.plain[build] }

func (standIn *standInImporter) Timing() (decode time.Duration, merge time.Duration) {
	return standIn.decode, 0
}

func (plain *plainSourceImporter) Import(path string) (*types.Package, error) {
	if path == "unsafe" {
		return types.Unsafe, nil
	}
	if checked, isChecked := plain.checked[path]; isChecked {
		return checked, nil
	}
	listed := plain.standIn.builds[plain.build].packages[path]
	if listed == nil {
		return nil, fmt.Errorf("stand-in importer: build %d lists no %s", plain.build, path)
	}
	started := time.Now()
	plain.standIn.depth++
	defer func() {
		plain.standIn.depth--
		if plain.standIn.depth == 0 {
			plain.standIn.decode += time.Since(started)
		}
	}()
	files := make([]*ast.File, 0, len(listed.CompiledGoFiles))
	for _, name := range listed.CompiledGoFiles {
		parsed, parseError := parser.ParseFile(plain.standIn.fileSet, name, nil, parser.SkipObjectResolution)
		if parseError != nil {
			return nil, fmt.Errorf("stand-in importer: %w", parseError)
		}
		files = append(files, parsed)
	}
	goVersion := ""
	if listed.Module != nil && listed.Module.GoVersion != "" {
		goVersion = "go" + listed.Module.GoVersion
	}
	configuration := &types.Config{
		Importer:  resolvingImporter{importer: plain, importMap: listed.ImportMap},
		Sizes:     plain.standIn.sizes,
		GoVersion: goVersion,
		Error: func(reported error) {
			plain.standIn.errors++
			if len(plain.standIn.firstErrors) < firstErrorsKept {
				plain.standIn.firstErrors = append(plain.standIn.firstErrors, reported.Error())
			}
		},
	}
	// Ignored: every error went to Error above, and a package with errors still answers what it could check.
	checked, _ := configuration.Check(path, plain.standIn.fileSet, files, nil)
	plain.checked[path] = checked
	return checked, nil
}
