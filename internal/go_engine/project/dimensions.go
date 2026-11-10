package project

import (
	"fmt"
	"go/build"
	"go/build/constraint"
	"go/parser"
	"go/token"
	"go/types"
	"go/version"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// Dimensions are what a set of files' build constraints vary on, from their `//go:build` (or `// +build`) lines
// and their file-name suffixes. They propose what to try, and never decide what a build compiles: that is always
// go/build's MatchFile on the file itself (see compiles), so reading a line here that go/build would not read only
// costs a try.
type Dimensions struct {
	// OperatingSystems are every `GOOS` the files name, `unix` read as each `GOOS` of the ports go/build counts as
	// unix, sorted.
	OperatingSystems []string

	// Architectures are every `GOARCH` the files name, gccgo's among them, sorted. The builds stay on the host's
	// architecture (see BuildsFor), so a file only one of these builds is a layout blind spot, never a build.
	Architectures []string

	// Tags are the custom tags the files name, sorted: every tag that is no `GOOS`, `GOARCH`, `unix`, `cgo`,
	// `ignore`, release tag (`go1.N`), compiler, or tag the toolchain sets itself (`goexperiment.X`,
	// `arm64.v8.0`).
	Tags []string

	// Cgo is whether a file names `cgo` or imports "C".
	Cgo bool
}

// unknownTarget is a `GOOS` and `GOARCH` no port has and go/build knows as neither, so a name go/build reads as a
// `GOOS` or `GOARCH` matches under its own value and never under this one.
const unknownTarget = "cohere"

// known is what go/build knows a value as.
type known int

const (
	knownAsNeither known = iota
	knownOperatingSystem
	knownArchitecture
)

// DimensionsOf reads files' dimensions, absolute paths, from disk, `unix` expanded over ports.
func DimensionsOf(files []string, ports []Port) (Dimensions, error) {
	sources := make([]source, 0, len(files))
	for _, file := range files {
		read, readError := readSource(file)
		if readError != nil {
			return Dimensions{}, readError
		}
		sources = append(sources, read)
	}
	return dimensionsOf(sources, ports), nil
}

// source is a census file's bytes, read once, and what its imports say.
type source struct {
	file     string
	contents []byte

	// importsC is whether the file imports "C", which go/build's MatchFile keeps with cgo off and only its Import
	// drops (see compiles).
	importsC bool
}

func readSource(file string) (source, error) {
	contents, readError := os.ReadFile(file)
	if readError != nil {
		return source{}, fmt.Errorf("reading %s: %w", file, readError)
	}
	return source{file: file, contents: contents, importsC: importsC(file, contents)}, nil
}

// importsC reports whether a file's imports name "C", read as load's exclusionReason reads them: a header that
// does not parse yields the imports read before the error, and the load reports the error itself.
func importsC(file string, contents []byte) bool {
	// Ignored: only the imports read decide, as above.
	header, _ := parser.ParseFile(token.NewFileSet(), file, contents, parser.ImportsOnly)
	if header == nil {
		return false
	}
	for _, imported := range header.Imports {
		// An import path can be written as a raw string, so it is compared unquoted.
		if importPath, unquoteError := strconv.Unquote(imported.Path.Value); unquoteError == nil && importPath == "C" {
			return true
		}
	}
	return false
}

// reading is one file's dimensions, and whether its constraint names `ignore`, which an unbuilt file's reason says.
type reading struct {
	operatingSystems []string
	architectures    []string
	tags             []string
	cgo              bool
	ignore           bool
}

// dimensionsOf merges each source's reading.
func dimensionsOf(sources []source, ports []Port) Dimensions {
	unix := unixOperatingSystems(ports)
	var dimensions Dimensions
	for _, read := range sources {
		fileReading := readingOf(read, unix)
		dimensions.OperatingSystems = append(dimensions.OperatingSystems, fileReading.operatingSystems...)
		dimensions.Architectures = append(dimensions.Architectures, fileReading.architectures...)
		dimensions.Tags = append(dimensions.Tags, fileReading.tags...)
		dimensions.Cgo = dimensions.Cgo || fileReading.cgo
	}
	dimensions.OperatingSystems = sortedSet(dimensions.OperatingSystems)
	dimensions.Architectures = sortedSet(dimensions.Architectures)
	dimensions.Tags = sortedSet(dimensions.Tags)
	return dimensions
}

// readingOf is what one file names: its name's `GOOS` and `GOARCH` as go/build reads the suffix, and every tag of
// its constraint lines, classified by tagReading, unix being the `GOOS` values `unix` stands for.
func readingOf(read source, unix []string) reading {
	var fileReading reading
	fileReading.operatingSystems, fileReading.architectures = namedBySuffix(filepath.Base(read.file))
	fileReading.cgo = read.importsC
	for _, expression := range constraintsOf(read) {
		for _, tag := range namedTags(expression, nil) {
			tagReading(tag, unix, &fileReading)
		}
	}
	fileReading.operatingSystems = sortedSet(fileReading.operatingSystems)
	fileReading.architectures = sortedSet(fileReading.architectures)
	fileReading.tags = sortedSet(fileReading.tags)
	return fileReading
}

// constraintsOf is a file's constraint lines, parsed: its `//go:build` lines before the package clause, or its
// `// +build` lines when it has none, as go/build prefers the one to the others. A line that does not parse is
// left out, since a dimension only proposes a try, and MatchFile reports the line's error itself.
func constraintsOf(read source) []constraint.Expr {
	// Ignored: a header that does not parse still yields the comments read before the error.
	header, _ := parser.ParseFile(token.NewFileSet(), read.file, read.contents,
		parser.PackageClauseOnly|parser.ParseComments)
	if header == nil {
		return nil
	}
	var goBuild []constraint.Expr
	var plusBuild []constraint.Expr
	for _, group := range header.Comments {
		if group.Pos() >= header.Package {
			break
		}
		for _, comment := range group.List {
			isGoBuild, isPlusBuild := constraint.IsGoBuild(comment.Text), constraint.IsPlusBuild(comment.Text)
			if !isGoBuild && !isPlusBuild {
				continue
			}
			expression, parseError := constraint.Parse(comment.Text)
			if parseError != nil {
				continue
			}
			if isGoBuild {
				goBuild = append(goBuild, expression)
			} else {
				plusBuild = append(plusBuild, expression)
			}
		}
	}
	if len(goBuild) > 0 {
		return goBuild
	}
	return plusBuild
}

// namedTags is every tag an expression names, appended to tags.
func namedTags(expression constraint.Expr, tags []string) []string {
	switch typed := expression.(type) {
	case *constraint.TagExpr:
		return append(tags, typed.Tag)
	case *constraint.NotExpr:
		return namedTags(typed.X, tags)
	case *constraint.AndExpr:
		return namedTags(typed.Y, namedTags(typed.X, tags))
	case *constraint.OrExpr:
		return namedTags(typed.Y, namedTags(typed.X, tags))
	}
	return tags
}

// tagReading adds what one constraint tag is to a file's reading. Whether a tag is a `GOOS` or a `GOARCH` is
// go/build's answer (see knownAs), never a list of ours. An architecture feature tag is spelled `<GOARCH>.<level>`
// (`amd64.v3`, cmd/go's ToolTags), so it is read as the architecture it needs.
//
// Mutates: fileReading gains the tag's dimension, if it is one
func tagReading(tag string, unix []string, fileReading *reading) {
	switch {
	case tag == "cgo":
		fileReading.cgo = true
		return
	case tag == "unix":
		fileReading.operatingSystems = append(fileReading.operatingSystems, unix...)
		return
	case tag == "ignore":
		fileReading.ignore = true
		return
	case version.IsValid(tag), slices.Contains(build.Default.ToolTags, tag), strings.HasPrefix(tag, "goexperiment."),
		tag == build.Default.Compiler, tag == "gccgo":
		// The release, the experiments and the compiler are the toolchain's, set by no build of ours.
		return
	}
	if knownAs(tag) == knownOperatingSystem {
		fileReading.operatingSystems = append(fileReading.operatingSystems, tag)
		return
	}
	if architecture, _, _ := strings.Cut(tag, "."); knownAs(architecture) == knownArchitecture ||
		types.SizesFor("gccgo", architecture) != nil {
		// gccgo targets architectures go/build does not know, `m68k` and `shbe` among them, and std's constraints
		// name them beside gc's (encoding/binary's big-endian file). The toolchain's own table of them is the sizes
		// go/types holds for gccgo, so such a name is read as the architecture it is, never as a tag a build may set.
		fileReading.architectures = append(fileReading.architectures, architecture)
		return
	}
	fileReading.tags = append(fileReading.tags, tag)
}

// knownAs is whether go/build knows a value as a `GOOS`, as a `GOARCH`, or as neither, by its own lists (go/build's
// unexported knownOS and knownArch), asked through file names rather than restated. A file named `p_<value>.go` is
// constrained, failing under unknownTarget for both, only when the value is known as either. And one named
// `p_<value>_<GOARCH>.go`, for an architecture go/build surely knows, the one this binary runs on, needs both only
// when the value is a `GOOS`: go/build reads two elements as a pair only when the first is a known `GOOS`, and the
// last alone otherwise. A value holding `_` or `.` is never one element of a name, so it is neither.
//
// Both roles are asked about with unknownTarget in the other slot, never a value of the other kind, since go/build
// matches a name's element against `GOOS` and `GOARCH` alike, and `GOARCH=windows` would satisfy `_windows.go`.
func knownAs(value string) known {
	if value == "" || strings.ContainsAny(value, "_.") {
		return knownAsNeither
	}
	if nameMatches("p_"+value+".go", unknownTarget, unknownTarget) {
		return knownAsNeither
	}
	if !nameMatches("p_"+value+"_"+build.Default.GOARCH+".go", unknownTarget, build.Default.GOARCH) {
		return knownOperatingSystem
	}
	return knownArchitecture
}

// namedBySuffix is the `GOOS` and `GOARCH` a file name's suffix names, as go/build reads it. The rule (which of the
// last one or two `_` elements count, `_test` stripped) is go/build's own, unexported goodOSArchFile, so it is
// asked rather than restated: MatchFile, handed the name with a bare package clause for its bytes, answers from the
// name alone. A name that matches under unknownTarget for both names nothing. Otherwise each element go/build knows
// as a `GOOS` counts where the answer turns on it, under unknownTarget or each known `GOARCH` element for the
// architecture, and each known `GOARCH` element alike, so a value is never tried in the other's slot.
func namedBySuffix(name string) ([]string, []string) {
	stem, _, _ := strings.Cut(name, ".")
	_, suffix, hasSuffix := strings.Cut(stem, "_")
	if !hasSuffix || nameMatches(name, unknownTarget, unknownTarget) {
		return nil, nil
	}
	var knownOperatingSystems []string
	var knownArchitectures []string
	for _, element := range strings.Split(suffix, "_") {
		switch knownAs(element) {
		case knownOperatingSystem:
			knownOperatingSystems = append(knownOperatingSystems, element)
		case knownArchitecture:
			knownArchitectures = append(knownArchitectures, element)
		case knownAsNeither:
		}
	}
	var operatingSystems []string
	var architectures []string
	for _, operatingSystem := range knownOperatingSystems {
		for _, architecture := range append(slices.Clone(knownArchitectures), unknownTarget) {
			if nameMatches(name, operatingSystem, architecture) && !nameMatches(name, unknownTarget, architecture) {
				operatingSystems = append(operatingSystems, operatingSystem)
			}
		}
	}
	for _, architecture := range knownArchitectures {
		for _, operatingSystem := range append(slices.Clone(knownOperatingSystems), unknownTarget) {
			if nameMatches(name, operatingSystem, architecture) && !nameMatches(name, operatingSystem, unknownTarget) {
				architectures = append(architectures, architecture)
			}
		}
	}
	return sortedSet(operatingSystems), sortedSet(architectures)
}

// bareFile is a Go file with no constraint, which a probe hands MatchFile so only a name can decide.
const bareFile = "package p\n"

// nameMatches is MatchFile's answer for a file of that name and no constraint, under operatingSystem and
// architecture.
func nameMatches(name string, operatingSystem string, architecture string) bool {
	return probeMatches(name, bareFile, operatingSystem, architecture)
}

// probeMatches is MatchFile's answer for a file of that name and those bytes, under operatingSystem and
// architecture, cgo off and no tags.
func probeMatches(name string, contents string, operatingSystem string, architecture string) bool {
	context := build.Default
	context.GOOS = operatingSystem
	context.GOARCH = architecture
	context.CgoEnabled = false
	context.BuildTags = nil
	context.OpenFile = func(string) (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader(contents)), nil
	}
	// Ignored: the bytes are a package clause and at most one well-formed constraint line, so nothing can fail.
	matched, _ := context.MatchFile("", name)
	return matched
}

// unixOperatingSystems is each `GOOS` of ports that `unix` matches, by go/build's own list: a file constrained to
// `unix` alone is asked about under each, sorted.
func unixOperatingSystems(ports []Port) []string {
	var unix []string
	for _, port := range ports {
		if probeMatches("p.go", "//go:build unix\n\n"+bareFile, port.OperatingSystem, port.Architecture) {
			unix = append(unix, port.OperatingSystem)
		}
	}
	return sortedSet(unix)
}

// sortedSet is values sorted with each once.
func sortedSet(values []string) []string {
	slices.Sort(values)
	return slices.Compact(values)
}
