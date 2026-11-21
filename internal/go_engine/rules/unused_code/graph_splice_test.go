package unused_code

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"reflect"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/analysis/facts/generated"
)

// checkedPackage is what a graph of one package is built from: its syntax in its file set, its types, and its
// generated files.
type checkedPackage struct {
	name           string
	fileSet        *token.FileSet
	files          []*ast.File
	typesPackage   *types.Package
	typesInfo      *types.Info
	generatedFiles map[string]generated.Generator
}

// checkShapes parses and type-checks sources as one package, each file in generatedNames a generated one. Every call
// parses afresh, into a file set of its own.
func checkShapes(test *testing.T, name string, sources []recordedSource, generatedNames []string) checkedPackage {
	test.Helper()
	checked := checkedPackage{name: name, fileSet: token.NewFileSet(), generatedFiles: map[string]generated.Generator{}}
	for _, each := range sources {
		file, parseError := parser.ParseFile(checked.fileSet, each.name, each.source, parser.ParseComments)
		if parseError != nil {
			test.Fatalf("parsing %s: %v", each.name, parseError)
		}
		checked.files = append(checked.files, file)
	}
	checked.typesInfo = &types.Info{
		Defs:       map[*ast.Ident]types.Object{},
		Uses:       map[*ast.Ident]types.Object{},
		Implicits:  map[ast.Node]types.Object{},
		Scopes:     map[ast.Node]*types.Scope{},
		Selections: map[*ast.SelectorExpr]*types.Selection{},
		Types:      map[ast.Expr]types.TypeAndValue{},
		Instances:  map[*ast.Ident]types.Instance{},
	}
	configuration := &types.Config{Importer: unsafeImporter{}}
	typesPackage, checkError := configuration.Check("example.com/shape", checked.fileSet, checked.files,
		checked.typesInfo)
	if checkError != nil {
		test.Fatalf("type-checking the package: %v", checkError)
	}
	checked.typesPackage = typesPackage
	for _, generatedName := range generatedNames {
		checked.generatedFiles[generatedName] = generated.Unknown
	}
	return checked
}

// fixturePackages is every package of the copy of the original's testdata, test variants included, as the analyzer
// was handed each.
func fixturePackages(test *testing.T) []checkedPackage {
	test.Helper()
	actions, runError := fixtureRun()
	if runError != nil {
		test.Fatal(runError)
	}
	checked := []checkedPackage{}
	for _, action := range actions.port {
		loaded := action.Package
		answer, generatedError := generated.Analyzer.Run(&analysis.Pass{Fset: loaded.Fset, Files: loaded.Syntax})
		if generatedError != nil {
			test.Fatalf("reading the generated files of %s: %v", loaded, generatedError)
		}
		generatedFiles, isGenerated := answer.(map[string]generated.Generator)
		if !isGenerated {
			test.Fatalf("the generated files of %s are %T", loaded, answer)
		}
		checked = append(checked, checkedPackage{
			name:           loaded.ID,
			fileSet:        loaded.Fset,
			files:          loaded.Syntax,
			typesPackage:   loaded.Types,
			typesInfo:      loaded.TypesInfo,
			generatedFiles: generatedFiles,
		})
	}
	return checked
}

// build is a graph of the package, entered, recording its bodies when records says to, and taking each of taken in
// place of walking its body.
func (checked checkedPackage) build(records bool, taken []bodyRecord) (*graph, error) {
	built := newGraph(checked.fileSet, checked.files, checked.typesPackage, checked.typesInfo,
		parseDirectives(checked.files, checked.fileSet), checked.generatedFiles)
	if records {
		built.recordBodies()
	}
	if taken != nil {
		if takeError := built.takeRecords(taken); takeError != nil {
			return nil, takeError
		}
	}
	if entryError := built.entry(); entryError != nil {
		return nil, entryError
	}
	return built, nil
}

// selfSpliced is one package built three ways (#xeafx79): plain, as the rule builds it; recording each body; and
// taking every record no flag keeps present in place of walking its body, each flagged body walked as present.
type selfSpliced struct {
	plain   *graph
	records []bodyRecord
	taken   []bodyRecord
	spliced *graph
}

// selfSplice builds checked's three graphs.
func (checked checkedPackage) selfSplice() (selfSpliced, error) {
	built := selfSpliced{taken: []bodyRecord{}}
	var buildError error
	if built.plain, buildError = checked.build(false, nil); buildError != nil {
		return selfSpliced{}, buildError
	}
	recording, recordError := checked.build(true, nil)
	if recordError != nil {
		return selfSpliced{}, recordError
	}
	built.records = recording.bodyRecords()
	for _, record := range built.records {
		if record.Present == (recordedPresence{}) {
			built.taken = append(built.taken, record)
		}
	}
	if built.spliced, buildError = checked.build(false, built.taken); buildError != nil {
		return selfSpliced{}, buildError
	}
	return built, nil
}

// spliceDifferences is how built's spliced graph differs from its plain one, each worded as a finding is: its nodes
// and edges counted, and the verdict's used, unused and quiet, each compared as a multiset of kind, name, parsed file
// and offset, and the unused, the rule's findings, in order too.
func spliceDifferences(built selfSpliced) []string {
	found := []string{}
	if len(built.plain.nodes) != len(built.spliced.nodes) || len(built.plain.uses) != len(built.spliced.uses) ||
		len(built.plain.owns) != len(built.spliced.owns) {
		found = append(found, fmt.Sprintf("the plain graph has %d nodes, %d uses and %d ownerships, the spliced %d, "+
			"%d and %d", len(built.plain.nodes), len(built.plain.uses), len(built.plain.owns),
			len(built.spliced.nodes), len(built.spliced.uses), len(built.spliced.owns)))
	}
	plain, spliced := built.plain.results(), built.spliced.results()
	fileSet := built.plain.fileSet
	plainUnused, splicedUnused := wordedObjects(fileSet, plain.unused), wordedObjects(fileSet, spliced.unused)
	found = append(found, multisetDifferences("used", wordedObjects(fileSet, plain.used),
		wordedObjects(fileSet, spliced.used))...)
	unusedDifferences := multisetDifferences("unused", plainUnused, splicedUnused)
	found = append(found, unusedDifferences...)
	found = append(found, multisetDifferences("quiet", wordedObjects(fileSet, plain.quiet),
		wordedObjects(fileSet, spliced.quiet))...)
	if len(unusedDifferences) == 0 && !slices.Equal(plainUnused, splicedUnused) {
		found = append(found, fmt.Sprintf("the unused are %v plain and %v spliced", plainUnused, splicedUnused))
	}
	return found
}

// wordedObjects is each of objects as its kind, its name, its parsed file and its offset.
func wordedObjects(fileSet *token.FileSet, objects []object) []string {
	worded := make([]string, 0, len(objects))
	for _, each := range objects {
		tokenFile := fileSet.File(each.position)
		worded = append(worded, fmt.Sprintf("%s %s at %s offset %d", each.kind, each.name, tokenFile.Name(),
			tokenFile.Offset(each.position)))
	}
	return worded
}

// multisetDifferences is each of plain not in spliced and each of spliced not in plain, counting repeats, so an
// object two nodes stand for is a difference from an object one does.
func multisetDifferences(list string, plain []string, spliced []string) []string {
	remaining := map[string]int{}
	for _, each := range spliced {
		remaining[each]++
	}
	found := []string{}
	for _, each := range plain {
		if remaining[each] > 0 {
			remaining[each]--
			continue
		}
		found = append(found, fmt.Sprintf("%s: %s plain, not spliced", list, each))
	}
	extra := []string{}
	for each, count := range remaining {
		for range count {
			extra = append(extra, fmt.Sprintf("%s: %s spliced, not plain", list, each))
		}
	}
	slices.Sort(extra)
	return append(found, extra...)
}

// spliceCase is a package where taking a body the wrong way changes the verdict, so each mutant of the splice is
// red: its files, the names the plain verdict finds unused in their order and quiet in any, and the body, by its
// function's name, a flag keeps present, if one does.
type spliceCase struct {
	name    string
	sources []recordedSource
	unused  []string
	quiet   []string
	flagged string
}

// spliceCases are the packages the splice needs that recordCases do not hold, each read against the walk.
var spliceCases = []spliceCase{
	{
		// A type used only inside one body: the used body's use reaches its type and leaves its other field
		// reported, and the unreached body's locals and its type's fields are quiet under their owners.
		name: "a type used only inside one body, with an unused field",
		sources: []recordedSource{{"shape.go", `package shape

type kept struct{ read, unread int }

type dropped struct{ read, unread int }

func Use() int {
	var value kept
	return value.read
}

func lonely() int {
	var value dropped
	return value.read
}
`}},
		unused: []string{"unread", "dropped", "lonely"},
		quiet:  []string{"read", "unread", "value"},
	},
	{
		// The owner chain into a body: a local constant nothing uses is reported where its function is used, and quiet
		// where its function is not.
		name: "a local const in a function nothing calls and in one something calls",
		sources: []recordedSource{{"shape.go", `package shape

func Called() { helper() }

func helper() {
	const counted = 1
}

func lonely() {
	const alone = 1
}
`}},
		unused: []string{"counted", "lonely"},
		quiet:  []string{"alone"},
	},
	{
		// The conversion's field to field edges are all that make the source's field used, which another function
		// reads only on the other side.
		name: "a struct conversion whose other side's field another function reads",
		sources: []recordedSource{{"shape.go", `package shape

type source struct{ value int }

type target struct{ value int }

func Convert(from source) target { return target(from) }

func Read(to target) int { return to.value }
`}},
		unused: []string{},
		quiet:  []string{},
	},
	{
		// The two fields share a name, so only their places tell which one the body reads.
		name: "two fields of one name, the second read",
		sources: []recordedSource{{"shape.go", `package shape

type first struct{ shared int }

type second struct{ shared int }

var One first

var Two second

func Read() int { return Two.shared }
`}},
		unused: []string{"shared"},
		quiet:  []string{},
	},
	{
		// The interface literal's unexported method is all that makes closer's method used, through the interfaces
		// the implements pass reads, which a record does not feed.
		name: "an interface literal with an unexported method, and the method only it makes used",
		sources: []recordedSource{{"shape.go", `package shape

type closer struct{}

func (closer) close() {}

var Value closer

func Check(value any) bool {
	_, held := value.(interface{ close() })
	return held
}
`}},
		unused:  []string{},
		quiet:   []string{},
		flagged: "Check",
	},
	{
		// The local type's method set is all that makes wrapper's embedded field used, through the selection of the
		// method it promotes, which the step over named types reads and a record does not feed.
		name: "a local type with a method set",
		sources: []recordedSource{{"shape.go", `package shape

type base struct{}

func (base) Method() {}

type wrapper struct{ base }

func Wrap() {
	type local wrapper
	var value local
	_ = value
}
`}},
		unused:  []string{},
		quiet:   []string{},
		flagged: "Wrap",
	},
	{
		// The directive is all that makes target's method and field used, through the type branch of the ignore step,
		// which reads the local alias's type from go/types and which a record's rooted locals do not feed.
		name: "an ignored local alias of a package-level type",
		sources: []recordedSource{{"shape.go", `package shape

type target struct{ field int }

func (target) method() {}

func IgnoredAlias() {
	//lint:ignore U1000 kept for the test
	type kept = target
}
`}},
		unused:  []string{},
		quiet:   []string{},
		flagged: "IgnoredAlias",
	},
	{
		// The first variable's field is a node only through ReadFirst's selection, and its place finds the second's,
		// so a record that names it by its place makes the graph one node short.
		name: "a selection of a field its place does not find",
		sources: []recordedSource{{"shape.go", `package shape

var first, second struct{ value int }

func ReadFirst() int { return first.value }

func ReadSecond() int { return second.value }
`}},
		unused:  []string{},
		quiet:   []string{},
		flagged: "ReadFirst",
	},
}

// TestSpliceCasesFindWhatTheyAreWrittenFor holds each splice case's plain verdict to what the case says it finds
// (#xeafx79), so a case still holds the shape a mutant of the splice is red over, and each flagged case's body to
// its flag.
func TestSpliceCasesFindWhatTheyAreWrittenFor(test *testing.T) {
	test.Parallel()
	for _, each := range spliceCases {
		test.Run(each.name, func(test *testing.T) {
			test.Parallel()
			built, buildError := checkShapes(test, each.name, each.sources, nil).selfSplice()
			if buildError != nil {
				test.Fatal(buildError)
			}
			verdict := built.plain.results()
			unused := []string{}
			for _, found := range verdict.unused {
				unused = append(unused, found.shortName)
			}
			quiet := []string{}
			for _, found := range verdict.quiet {
				quiet = append(quiet, found.shortName)
			}
			slices.Sort(quiet)
			if !slices.Equal(unused, each.unused) || !slices.Equal(quiet, each.quiet) {
				test.Errorf("the plain verdict finds %v unused and %v quiet, not %v and %v", unused, quiet, each.unused,
					each.quiet)
			}
			flagged := len(built.records) - len(built.taken)
			if each.flagged == "" && flagged != 0 {
				test.Errorf("%d bodies are flagged, where the case flags none", flagged)
			}
			if each.flagged != "" && flagged != 1 {
				test.Errorf("%d bodies are flagged, where the case flags %s alone", flagged, each.flagged)
			}
		})
	}
}

// TestSplicingAFlaggedBodyChangesTheVerdict holds each flag the splice refuses to its reason (#xeafx79): a flagged
// case's body, its flag cleared and taken all the same, gives another verdict than the walk of it does, since the
// flag marks what the walk of that body feeds the steps after it and a record does not hold.
func TestSplicingAFlaggedBodyChangesTheVerdict(test *testing.T) {
	test.Parallel()
	for _, each := range spliceCases {
		if each.flagged == "" {
			continue
		}
		test.Run(each.name, func(test *testing.T) {
			test.Parallel()
			checked := checkShapes(test, each.name, each.sources, nil)
			built, buildError := checked.selfSplice()
			if buildError != nil {
				test.Fatal(buildError)
			}
			cleared := []bodyRecord{}
			for _, record := range built.records {
				record.Present = recordedPresence{}
				cleared = append(cleared, record)
			}
			taken, takeError := checked.build(false, cleared)
			if takeError != nil {
				test.Fatal(takeError)
			}
			built.spliced = taken
			if found := spliceDifferences(built); len(found) == 0 {
				test.Errorf("taking %s with its flag cleared gives the plain verdict, so the flag keeps nothing",
					each.flagged)
			}
		})
	}
}

// refusedSource is a package of one body whose record names a local, an object declared outside it and the
// function, which TestSpliceRefusesWhatItCannotTake takes apart.
const refusedSource = `package shape

var counter int

func Count() int {
	const step = 1
	return counter + step
}
`

// TestSpliceRefusesWhatItCannotTake holds the splice to failing by name (#xeafx79), never dropping an edge and never
// panicking: a record each flag keeps present, two of one key, one a graph that records is handed, one keyed where no
// body is, an end it cannot resolve and a local the root owns, each an error naming the body, and the place where it
// has one.
func TestSpliceRefusesWhatItCannotTake(test *testing.T) {
	test.Parallel()
	checked := checkShapes(test, "refused", []recordedSource{{"shape.go", refusedSource}}, nil)
	built, buildError := checked.selfSplice()
	if buildError != nil {
		test.Fatal(buildError)
	}
	if len(built.taken) != 1 || len(built.taken[0].Locals) != 1 || len(built.taken[0].Uses) == 0 {
		test.Fatalf("the package takes %+v, not Count's one record of a local and its uses", built.taken)
	}
	count := built.taken[0]
	withRecord := func(change func(record *bodyRecord)) []bodyRecord {
		changed := count
		changed.Locals = slices.Clone(count.Locals)
		changed.Uses = slices.Clone(count.Uses)
		changed.Rooted = slices.Clone(count.Rooted)
		change(&changed)
		return []bodyRecord{changed}
	}
	unplaced := recordedPlace{File: "shape.go", Offset: strings.Index(refusedSource, "counter +")}
	type refusal struct {
		name    string
		taken   []bodyRecord
		records bool
		names   []string
	}
	cases := []refusal{
		{
			name:  "two records of one key",
			taken: []bodyRecord{count, count},
			names: []string{placeText(count.Body)},
		},
		{
			name:    "a record handed to a graph that records",
			taken:   []bodyRecord{count},
			records: true,
		},
		{
			name: "a record keyed where no body is",
			taken: withRecord(func(record *bodyRecord) {
				record.Body.Offset++
			}),
			names: []string{placeText(recordedPlace{File: count.Body.File, Offset: count.Body.Offset + 1})},
		},
		{
			name: "a declared end no definition is placed at",
			taken: withRecord(func(record *bodyRecord) {
				for index := range record.Uses {
					if record.Uses[index].Used.Kind == endDeclared {
						record.Uses[index].Used.Place = unplaced
					}
				}
			}),
			names: []string{placeText(count.Body), placeText(unplaced)},
		},
		{
			// The recorder never writes it, and the graph's first owners read the root's index as no owner at all.
			name: "a local the root owns",
			taken: withRecord(func(record *bodyRecord) {
				record.Locals[0].Owners = append(slices.Clone(record.Locals[0].Owners), recordedEnd{Kind: endRoot})
			}),
			names: []string{placeText(count.Body)},
		},
		{
			name: "a local past the record's locals",
			taken: withRecord(func(record *bodyRecord) {
				record.Rooted = append(record.Rooted, len(record.Locals))
			}),
			names: []string{placeText(count.Body)},
		},
	}
	// Each flag alone, read from the type, so a refusal that reads some of the flags and not the rest is red for
	// each it leaves out, and a flag added later is refused here without a case written for it.
	presence := reflect.TypeFor[recordedPresence]()
	for field := range presence.NumField() {
		cases = append(cases, refusal{
			name: "a record " + presence.Field(field).Name + " keeps present",
			taken: withRecord(func(record *bodyRecord) {
				reflect.ValueOf(&record.Present).Elem().Field(field).SetBool(true)
			}),
			names: []string{placeText(count.Body), presence.Field(field).Name + ":true"},
		})
	}
	for _, each := range cases {
		test.Run(each.name, func(test *testing.T) {
			test.Parallel()
			taken, takeError := checked.build(each.records, each.taken)
			if takeError == nil {
				test.Fatalf("the graph took the records, and holds %d nodes", len(taken.nodes))
			}
			for _, name := range each.names {
				if !strings.Contains(takeError.Error(), name) {
					test.Errorf("the error %q does not name %s", takeError, name)
				}
			}
		})
	}
}

// TestSpliceRefusesARecordTwoBodiesAreKeyedBy holds the splice to taking a record for one body alone (#xeafx79):
// handed the walk's record of First, whose key Second shares through a `//line` directive, the graph meets two bodies
// at that key and fails by it rather than splice the record in for either. The host keeps no record of such a body
// (bodyRecords), but a build's files are its own, and a key the host's package held one body at can hold two there.
func TestSpliceRefusesARecordTwoBodiesAreKeyedBy(test *testing.T) {
	test.Parallel()
	checked := checkShapes(test, "shared key", sharedKeySources, nil)
	recording, recordError := checked.build(true, nil)
	if recordError != nil {
		test.Fatal(recordError)
	}
	if len(recording.recorder.records) != 3 ||
		recording.recorder.records[0].Body != recording.recorder.records[1].Body {
		test.Fatalf("the walk recorded %+v, not First and Second at one key and Third", recording.recorder.records)
	}
	first := recording.recorder.records[0]
	taken, takeError := checked.build(false, []bodyRecord{first})
	if takeError == nil {
		test.Fatalf("the graph took First's record at a key two bodies share, and holds %d nodes", len(taken.nodes))
	}
	for _, name := range []string{placeText(first.Body), "declares 2 function bodies"} {
		if !strings.Contains(takeError.Error(), name) {
			test.Errorf("the error %q does not name %s", takeError, name)
		}
	}
}

// minimumSpliced is how many bodies TestSelfSpliceGivesThePlainVerdict spliced when it was written, of 290 it
// recorded over 89 packages, 18 flagged.
const minimumSpliced = 272

// TestSelfSpliceGivesThePlainVerdict is the self-splice (#xeafx79, the design's first machine): over every fixture
// package, test variants included, every recorder shape and every splice case, the graph that takes every record no
// flag keeps present, walking each flagged body as present, gives the plain graph's nodes, edges and verdict. It
// proves a record holds everything its body adds before any build leans on one. It is not empty-handed: it splices
// at least as many bodies as it did when written, and among them a local that is reported, a local quiet under its
// owner, a rooted local, a field to field use, and a declared end that is a field of a signature's anonymous struct.
func TestSelfSpliceGivesThePlainVerdict(test *testing.T) {
	test.Parallel()
	packages := fixturePackages(test)
	for _, each := range recordCases {
		packages = append(packages, checkShapes(test, each.name, each.sources, each.generated))
	}
	for _, each := range spliceCases {
		packages = append(packages, checkShapes(test, each.name, each.sources, nil))
	}
	counted := map[string]int{}
	for _, checked := range packages {
		built, buildError := checked.selfSplice()
		if buildError != nil {
			test.Errorf("%s: %v", checked.name, buildError)
			continue
		}
		for _, difference := range spliceDifferences(built) {
			test.Errorf("%s: %s", checked.name, difference)
		}
		for name, count := range spliceCounts(checked, built) {
			counted[name] += count
		}
	}
	test.Logf("%v", counted)
	if counted["spliced"] < minimumSpliced {
		test.Errorf("spliced %d bodies, fewer than the %d the test was written over", counted["spliced"],
			minimumSpliced)
	}
	for _, name := range []string{
		"reportedLocals", "quietLocals", "rootedLocals", "fieldToFieldUses", "signatureStructFields",
	} {
		if counted[name] == 0 {
			test.Errorf("the splices held no %s, so agreeing proves less than it reads: %v", name, counted)
		}
	}
}

// spliceCounts is what TestSelfSpliceGivesThePlainVerdict counts of built, a self-splice of checked, by name: the
// bodies recorded, spliced and flagged, the bodies each of the six flags kept present, the spliced locals reported
// and quiet, and the spliced shapes the test must meet.
func spliceCounts(checked checkedPackage, built selfSpliced) map[string]int {
	counted := map[string]int{
		"packages": 1,
		"bodies":   len(built.records),
		"spliced":  len(built.taken),
		"flagged":  len(built.records) - len(built.taken),
	}
	for _, record := range built.records {
		counted["unexportedInterfaceMethod"] += countOf(record.Present.UnexportedInterfaceMethod)
		counted["localMethodSet"] += countOf(record.Present.LocalMethodSet)
		counted["interfaceInstance"] += countOf(record.Present.InterfaceInstance)
		counted["ignoredType"] += countOf(record.Present.IgnoredType)
		counted["allFieldsUsed"] += countOf(record.Present.AllFieldsUsed)
		counted["unplacedEnd"] += countOf(record.Present.UnplacedEnd)
	}

	states := built.spliced.colorAndQuieten()
	for index := range built.spliced.splice.locals {
		switch {
		case states[index].seen():
		case states[index].quiet():
			counted["quietLocals"]++
		default:
			counted["reportedLocals"]++
		}
	}

	// A signature's span, in which a field of an anonymous struct is a field of a signature's.
	type span struct{ start, end token.Pos }
	signatures := []span{}
	for _, file := range checked.files {
		for _, declaration := range file.Decls {
			if function, isFunction := declaration.(*ast.FuncDecl); isFunction {
				signatures = append(signatures, span{function.Type.Pos(), function.Type.End()})
			}
		}
	}
	isField := func(record bodyRecord, end recordedEnd) bool {
		switch end.Kind {
		case endLocal:
			return record.Locals[end.Local].Kind == "field"
		case endDeclared:
			variable, isVariable := built.spliced.definedAt(end.Place).(*types.Var)
			return isVariable && variable.IsField()
		default:
			return false
		}
	}
	for _, record := range built.taken {
		counted["rootedLocals"] += len(record.Rooted)
		for _, use := range record.Uses {
			if isField(record, use.By) && isField(record, use.Used) {
				counted["fieldToFieldUses"]++
			}
			for _, end := range []recordedEnd{use.By, use.Used} {
				if end.Kind != endDeclared || !isField(record, end) {
					continue
				}
				position := built.spliced.definedAt(end.Place).Pos()
				if slices.ContainsFunc(signatures, func(each span) bool {
					return each.start <= position && position < each.end
				}) {
					counted["signatureStructFields"]++
				}
			}
		}
	}
	return counted
}

// countOf is 1 for a flag that is set and 0 for one that is not.
func countOf(isSet bool) int {
	if isSet {
		return 1
	}
	return 0
}
