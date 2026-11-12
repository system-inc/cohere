package check

import (
	"encoding/hex"
	"flag"
	"go/types"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/analysis/facts/deprecated"

	"github.com/system-inc/cohere/internal/go_engine/build_reads"
)

// updateRenderings has the golden test write the renderings it computes over the golden, after a change that moved
// them and ReadsRendererVersion with them.
var updateRenderings = flag.Bool("update", false, "write the reads renderings golden")

// renderingsGolden is where the golden of the fixture's renderings lives.
var renderingsGolden = filepath.Join("testdata", "reads_renderings.golden")

// renderingsSource declares what the verdict fixture does not: an alias, a generic alias, an embedded field, an
// interface with an unexported method, which keeps an unexported method of the package seen, and a constant of every
// kind.
const renderingsSource = "package rendered\n\n" +
	"type Base struct{ Count int }\n\n" +
	"type Outer struct {\n\tBase\n\tlabel string\n\tInner *Outer\n}\n\n" +
	"func (Outer) hidden() {}\n\n" +
	"func (*Outer) Shown() string { return \"\" }\n\n" +
	"type hider interface{ hidden() }\n\n" +
	"var Held hider\n\n" +
	"type Named = Outer\n\n" +
	"type Pair[Left any, Right comparable] struct {\n\tLeft  Left\n\tRight Right\n}\n\n" +
	"type Same[Value comparable] = Pair[Value, Value]\n\n" +
	"const Pi = 3.14159265358979323846264338327950288419716939937510582097494459\n\n" +
	"const Large = 1 << 80\n\n" +
	"const Rotation = 1.5 + 2i\n\n" +
	"const Greeting = \"hello\"\n"

// readsRenderingLines are what a persisted digest is taken of, for a fixed fixture: the version line, then each
// object of the verdict fixture's darwin platform and of renderingsSource, its closure digest measured by the amd64
// sizes and whole, each beside the closure it stands for written out (closureRendering), the digest of what each kind
// of use reads of it, measured, beside the lines it reads (useLines), and the value digests of fixed facts.
func readsRenderingLines(test *testing.T) []string {
	test.Helper()

	sizes := types.SizesFor("gc", "amd64")
	lines := []string{"renderer " + ReadsRendererVersion}
	for _, declaring := range []*types.Package{
		checkedWorld(test, "darwin", "").platform,
		checkedInMemory(test, renderingPath, renderingsSource, nil),
	} {
		measured, whole := NewReadsBuild(), NewReadsBuild()
		measured.measureBy(sizes)
		for _, name := range declaring.Scope().Names() {
			object := declaring.Scope().Lookup(name)
			for _, rendering := range []struct {
				label string
				memo  *ReadsBuild
				sizes types.Sizes
			}{{"measured", measured, sizes}, {"whole", whole, nil}} {
				digest := rendering.memo.closureDigest(object)
				lines = append(lines, rendering.label+" "+objectName(object)+" "+hex.EncodeToString(digest[:]))
				for _, line := range closureRendering(object, rendering.sizes) {
					lines = append(lines, "\t"+line)
				}
			}
			// What each kind of use reads of the object (#mjdf7ah): a type name's name, underlying type and method set,
			// and anything else's own line, which a selection's member is judged by too.
			kinds := []build_reads.UseKind{build_reads.UseMember}
			if _, isTypeName := object.(*types.TypeName); isTypeName {
				kinds = []build_reads.UseKind{build_reads.UseName, build_reads.UseUnderlying, build_reads.UseMethodSet}
			}
			for _, kind := range kinds {
				digest := measured.useDigest(object, kind)
				lines = append(lines, "use "+string(kind)+" "+objectName(object)+" "+hex.EncodeToString(digest[:]))
				for _, line := range measured.useLines(object, kind) {
					lines = append(lines, "\t"+line)
				}
			}
			// The own lines a Method or Interface lookup's candidate is compared by (#qqzg1w8), for each method a rule
			// could match it by.
			if typeName, isTypeName := object.(*types.TypeName); isTypeName {
				lines = append(lines, methodCandidateLines(typeName)...)
			}
		}
	}
	for _, fact := range []analysis.Fact{
		&valueFact{Count: 1, Names: []string{"first", "second"}, Weights: map[string]float64{"heavy": 1.5},
			Next: &valueFact{Count: 2}, When: time.Unix(1_700_000_000, 0).UTC()},
		&deprecated.IsDeprecated{Msg: "use Other"},
	} {
		digest, digestError := factValueDigest(fact)
		if digestError != nil {
			test.Fatal(digestError)
		}
		lines = append(lines, "fact "+factTypeName(reflect.TypeOf(fact))+" "+hex.EncodeToString(digest[:]))
	}
	return lines
}

// methodCandidateLines are typeName's own lines as a Method lookup's candidate, for each method a rule could match it
// by, an interface's every one and a named type's declared ones: the digest, then its method's signature and, for an
// interface, every method of its method set.
func methodCandidateLines(typeName *types.TypeName) []string {
	var methods []*types.Func
	if interfaceType, isInterface := typeName.Type().Underlying().(*types.Interface); isInterface {
		methods = slices.Collect(interfaceType.Methods())
	} else if named, isNamed := types.Unalias(typeName.Type()).(*types.Named); isNamed {
		methods = slices.Collect(named.Methods())
	}
	var lines []string
	for _, method := range methods {
		candidate, isCandidate := methodCandidateOf(typeName, build_reads.QueryMethod, method.Name(), nil,
			ReadsRendererVersion)
		if !isCandidate {
			continue
		}
		lines = append(lines,
			"candidate "+objectName(typeName)+"."+method.Name()+" "+hex.EncodeToString(candidate.lines),
			"\t"+signatureLine(method))
		if interfaceType, isInterface := typeName.Type().Underlying().(*types.Interface); isInterface {
			lines = append(lines, "\t"+strings.ReplaceAll(interfaceLine(interfaceType), "\n", "\n\t"))
		}
	}
	return lines
}

// The renderings a persisted digest is taken of stay as the golden holds them while ReadsRendererVersion does, so a
// change to a closure digest, a rendering or a fact's value digest that does not move the version fails here: a record
// another binary persisted would otherwise be matched against renderings made another way. A change that moves the
// version too writes the golden again with -update.
func TestTheReadsRenderingsMoveOnlyWithTheirVersion(test *testing.T) {
	test.Parallel()

	computed := strings.Join(readsRenderingLines(test), "\n") + "\n"
	if *updateRenderings {
		if writeError := os.WriteFile(renderingsGolden, []byte(computed), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
		return
	}
	golden, readError := os.ReadFile(renderingsGolden)
	if readError != nil {
		test.Fatalf("reading the golden: %v (write it with -update)", readError)
	}
	if string(golden) == computed {
		return
	}
	goldenVersion, _, _ := strings.Cut(string(golden), "\n")
	if goldenVersion == "renderer "+ReadsRendererVersion {
		test.Fatalf("the reads renderings moved while ReadsRendererVersion stayed %s: move it, then write the "+
			"golden again with -update. The first line that moved:\n%s", ReadsRendererVersion,
			firstMovedLine(string(golden), computed))
	}
	test.Fatalf("the golden is of %s, while ReadsRendererVersion is %s: write it again with -update",
		strings.TrimPrefix(goldenVersion, "renderer "), ReadsRendererVersion)
}

// firstMovedLine is the first line golden and computed write differently, each side's.
func firstMovedLine(golden string, computed string) string {
	goldenLines, computedLines := strings.Split(golden, "\n"), strings.Split(computed, "\n")
	for index := range max(len(goldenLines), len(computedLines)) {
		was := ""
		if index < len(goldenLines) {
			was = goldenLines[index]
		}
		now := ""
		if index < len(computedLines) {
			now = computedLines[index]
		}
		if was != now {
			return "golden   " + was + "\ncomputed " + now
		}
	}
	return ""
}
