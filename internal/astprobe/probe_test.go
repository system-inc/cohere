// Package astprobe answers one question and then gets deleted or grows into a cache.
//
// The question, from #5n9as3b: can typescript-go's AST be written to disk and read back
// decisively cheaper than re-parsing the source? Not "can it be serialized" — anything can.
// Whether reading beats the thing it replaces, because a cache that costs more to read than
// to rebuild is a real outcome rather than a hypothetical.
//
// The subject is lib.dom.d.ts at 2.35 MB, which is in the program on every run and is the
// kind of declaration file the cache would target. A 200-line fixture round-trips fast for
// reasons that do not survive at 78 MB.
package astprobe

import (
	"encoding/binary"
	"fmt"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/core"
	"github.com/microsoft/TypeScript/tsc/shim/parser"
	"github.com/microsoft/TypeScript/tsc/shim/tspath"
)

// subjectPath is lib.dom.d.ts, 2.35 MB, the largest declaration file in the tree and one that
// every run of verify parses. Named here rather than discovered so the report can state it.
const subjectPath = "/Users/kirkouimet/Projects/ahra/node_modules/.pnpm/typescript@6.0.3/node_modules/typescript/lib/lib.dom.d.ts"

// flatNode is what a cache would actually store: no pointers, no interface, fixed width.
// Children are a contiguous run in the same array, which is the only layout that lets a
// reader page the file in and walk it without allocating per node.
//
// This is deliberately the most favorable shape available to the cache. If the optimistic
// format loses, no realistic one wins.
type flatNode struct {
	Kind        int32
	Flags       int32
	Pos         int32
	End         int32
	ParentIndex int32
	FirstChild  int32
	ChildCount  int32
	_           int32 // pad to 32 bytes so the array is cache-line friendly
}

const flatNodeSize = 32

// flatten walks the parsed tree into the array above. This is the write path and it is paid
// once on a cold run, so it is allowed to be slow.
func flatten(sourceFile *ast.SourceFile) []flatNode {
	// A 2.35 MB declaration file lands near 400k nodes; preallocating keeps the write path
	// from dominating a measurement that is not about the write path.
	nodes := make([]flatNode, 0, 512*1024)

	var visit func(node *ast.Node, parentIndex int32)
	visit = func(node *ast.Node, parentIndex int32) {
		selfIndex := int32(len(nodes))
		nodes = append(nodes, flatNode{
			Kind:        int32(node.Kind),
			Flags:       int32(node.Flags),
			Pos:         int32(node.Pos()),
			End:         int32(node.End()),
			ParentIndex: parentIndex,
			FirstChild:  -1,
			ChildCount:  0,
		})

		firstChild := int32(len(nodes))
		count := int32(0)
		node.ForEachChild(func(child *ast.Node) bool {
			visit(child, selfIndex)
			count++
			return false
		})

		// Children are not contiguous under a depth-first walk (a child's own subtree lands
		// between it and its sibling), so FirstChild/ChildCount describe the direct-children
		// span only when the subtree is flat. Recorded anyway: the read path below does not
		// rely on it, and a real format would use a separate child-index array. Keeping the
		// field means the write measurement includes the cost of producing it.
		if count > 0 {
			nodes[selfIndex].FirstChild = firstChild
			nodes[selfIndex].ChildCount = count
		}
	}

	visit(sourceFile.AsNode(), -1)
	return nodes
}

// encode writes the flat array as raw little-endian bytes. No framing, no compression, no
// varints: the cache would mmap this region, and this is the cheapest possible write.
func encode(nodes []flatNode) []byte {
	buffer := make([]byte, len(nodes)*flatNodeSize)
	for index, node := range nodes {
		offset := index * flatNodeSize
		binary.LittleEndian.PutUint32(buffer[offset+0:], uint32(node.Kind))
		binary.LittleEndian.PutUint32(buffer[offset+4:], uint32(node.Flags))
		binary.LittleEndian.PutUint32(buffer[offset+8:], uint32(node.Pos))
		binary.LittleEndian.PutUint32(buffer[offset+12:], uint32(node.End))
		binary.LittleEndian.PutUint32(buffer[offset+16:], uint32(node.ParentIndex))
		binary.LittleEndian.PutUint32(buffer[offset+20:], uint32(node.FirstChild))
		binary.LittleEndian.PutUint32(buffer[offset+24:], uint32(node.ChildCount))
	}
	return buffer
}

// decodeFlat is the optimistic read: bytes to a flat array, no pointer graph built.
// This is the number a mmap'd cache would get if rules could walk indices instead of nodes.
func decodeFlat(buffer []byte) []flatNode {
	count := len(buffer) / flatNodeSize
	nodes := make([]flatNode, count)
	for index := range nodes {
		offset := index * flatNodeSize
		nodes[index] = flatNode{
			Kind:        int32(binary.LittleEndian.Uint32(buffer[offset+0:])),
			Flags:       int32(binary.LittleEndian.Uint32(buffer[offset+4:])),
			Pos:         int32(binary.LittleEndian.Uint32(buffer[offset+8:])),
			End:         int32(binary.LittleEndian.Uint32(buffer[offset+12:])),
			ParentIndex: int32(binary.LittleEndian.Uint32(buffer[offset+16:])),
			FirstChild:  int32(binary.LittleEndian.Uint32(buffer[offset+20:])),
			ChildCount:  int32(binary.LittleEndian.Uint32(buffer[offset+24:])),
		}
	}
	return nodes
}

// rebuiltNode is the honest read: what a rule actually needs. Rules walk *ast.Node — they
// read node.Parent, they type-switch on data, they call helpers that take a node. A flat
// array is not that, so a cache that stops at decodeFlat has not replaced parsing; it has
// replaced parsing with something rules cannot consume.
//
// This measures the floor of turning the flat array back into a pointer graph: one
// allocation per node and one parent link. It does NOT reconstruct the nodeData interface,
// which is the expensive part a real implementation would also owe, so this number is
// optimistic in the cache's favor.
type rebuiltNode struct {
	Kind     int32
	Flags    int32
	Pos      int32
	End      int32
	Parent   *rebuiltNode
	Children []*rebuiltNode
}

func rebuild(flat []flatNode) []rebuiltNode {
	nodes := make([]rebuiltNode, len(flat))
	for index := range flat {
		nodes[index].Kind = flat[index].Kind
		nodes[index].Flags = flat[index].Flags
		nodes[index].Pos = flat[index].Pos
		nodes[index].End = flat[index].End
		if flat[index].ParentIndex >= 0 {
			parent := &nodes[flat[index].ParentIndex]
			nodes[index].Parent = parent
			parent.Children = append(parent.Children, &nodes[index])
		}
	}
	return nodes
}

func readSource(t *testing.T) string {
	t.Helper()
	bytes, err := os.ReadFile(subjectPath)
	if err != nil {
		t.Skipf("subject not on disk, probe cannot run: %v", err)
	}
	return string(bytes)
}

func parseOnce(sourceText string) *ast.SourceFile {
	fileName := tspath.NormalizePath(subjectPath)
	return parser.ParseSourceFile(ast.SourceFileParseOptions{
		FileName: fileName,
		Path:     tspath.Path(fileName),
	}, sourceText, core.ScriptKindTS)
}

// loadAverage reports the 1-minute load so every reading below carries the machine's state.
// Seven consecutive runs of the same binary on this tree once ranged 3.14s to 12.24s at
// load 23; a timing without a load beside it is not a measurement.
func loadAverage() string {
	bytes, err := os.ReadFile("/proc/loadavg")
	if err == nil {
		return string(bytes[:4])
	}
	// darwin has no /proc; sysctl is read by the caller script instead. Reported as unknown
	// rather than guessed, since a fabricated load is worse than an absent one.
	return "see report"
}

func median(durations []time.Duration) time.Duration {
	sorted := append([]time.Duration(nil), durations...)
	for i := 1; i < len(sorted); i++ {
		for j := i; j > 0 && sorted[j] < sorted[j-1]; j-- {
			sorted[j], sorted[j-1] = sorted[j-1], sorted[j]
		}
	}
	return sorted[len(sorted)/2]
}

const runs = 7

func TestProbeSerializedAstAgainstParsing(t *testing.T) {
	sourceText := readSource(t)

	stat, err := os.Stat(subjectPath)
	if err != nil {
		t.Fatalf("stat subject: %v", err)
	}

	// One parse up front, to size the artifact and to prove the walk sees a real tree.
	// A probe that measured an empty or truncated parse would report beautifully.
	warmup := parseOnce(sourceText)
	if warmup == nil {
		t.Fatal("subject did not parse; the probe has no input")
	}
	flat := flatten(warmup)
	encoded := encode(flat)

	// Prove the input is real. 2.35 MB of declarations is not 12 nodes, and a probe that
	// measured a stub would produce a clean, fast, meaningless number.
	if len(flat) < 10_000 {
		t.Fatalf("walk produced %d nodes for a %d-byte file; the probe is measuring a stub, not the tree",
			len(flat), stat.Size())
	}

	temporaryPath := t.TempDir() + "/subject.astbin"
	if err := os.WriteFile(temporaryPath, encoded, 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}

	var parseTimings, writeTimings, readFlatTimings, readRebuildTimings []time.Duration

	for run := 0; run < runs; run++ {
		runtime.GC()

		start := time.Now()
		parsed := parseOnce(sourceText)
		parseTimings = append(parseTimings, time.Since(start))
		if parsed == nil {
			t.Fatal("parse returned nil mid-measurement")
		}

		runtime.GC()
		start = time.Now()
		written := encode(flatten(parsed))
		writeTimings = append(writeTimings, time.Since(start))
		if len(written) != len(encoded) {
			t.Fatalf("write path is not deterministic: %d bytes then %d", len(encoded), len(written))
		}

		runtime.GC()
		start = time.Now()
		raw, err := os.ReadFile(temporaryPath)
		if err != nil {
			t.Fatalf("read artifact: %v", err)
		}
		decoded := decodeFlat(raw)
		readFlatTimings = append(readFlatTimings, time.Since(start))

		runtime.GC()
		start = time.Now()
		raw, err = os.ReadFile(temporaryPath)
		if err != nil {
			t.Fatalf("read artifact: %v", err)
		}
		rebuilt := rebuild(decodeFlat(raw))
		readRebuildTimings = append(readRebuildTimings, time.Since(start))

		if len(decoded) != len(flat) || len(rebuilt) != len(flat) {
			t.Fatalf("round trip lost nodes: wrote %d, decoded %d, rebuilt %d",
				len(flat), len(decoded), len(rebuilt))
		}
	}

	parseMedian := median(parseTimings)
	readFlatMedian := median(readFlatTimings)
	readRebuildMedian := median(readRebuildTimings)

	fmt.Printf("\n=== #5n9as3b probe ===\n")
	fmt.Printf("subject      %s\n", subjectPath)
	fmt.Printf("source       %.2f MB\n", float64(stat.Size())/(1024*1024))
	fmt.Printf("nodes        %d\n", len(flat))
	fmt.Printf("artifact     %.2f MB (%d bytes/node, flat, no strings)\n",
		float64(len(encoded))/(1024*1024), flatNodeSize)
	fmt.Printf("load         %s\n", loadAverage())
	fmt.Printf("runs         %d, medians below\n\n", runs)

	fmt.Printf("parse from source        %8.2f ms   the thing we are trying to beat\n",
		float64(parseMedian.Microseconds())/1000)
	fmt.Printf("serialize (write)        %8.2f ms   paid once, cold run, allowed to be slow\n",
		float64(median(writeTimings).Microseconds())/1000)
	fmt.Printf("read back, flat only     %8.2f ms   %.2fx vs parse (rules cannot walk this)\n",
		float64(readFlatMedian.Microseconds())/1000,
		float64(parseMedian)/float64(readFlatMedian))
	fmt.Printf("read back, usable graph  %8.2f ms   %.2fx vs parse (THE VERDICT NUMBER)\n",
		float64(readRebuildMedian.Microseconds())/1000,
		float64(parseMedian)/float64(readRebuildMedian))
	fmt.Printf("\nall timings, parse then usable-read, per run:\n")
	for run := 0; run < runs; run++ {
		fmt.Printf("  run %d  parse %7.2f ms   read %7.2f ms\n", run+1,
			float64(parseTimings[run].Microseconds())/1000,
			float64(readRebuildTimings[run].Microseconds())/1000)
	}
	fmt.Printf("\n")
}

// TestProbeDetectorCanFail proves the round trip can report a mismatch. A probe whose
// comparison has never once failed has not been shown to compare anything: the node-count
// check in the main test would pass just as happily against a corrupted artifact if the
// check were vacuous. Feed it a known-bad artifact and confirm it notices.
func TestProbeDetectorCanFail(t *testing.T) {
	sourceText := readSource(t)
	flat := flatten(parseOnce(sourceText))
	encoded := encode(flat)

	// Truncate one node off the end. A decoder that ignores its input would still return
	// len(flat) and this test would fail, which is the point.
	corrupted := encoded[:len(encoded)-flatNodeSize]
	decoded := decodeFlat(corrupted)

	if len(decoded) == len(flat) {
		t.Fatalf("decoder returned %d nodes from a truncated artifact holding %d; "+
			"the round-trip check cannot detect loss and every clean result from it is vacuous",
			len(decoded), len(flat))
	}
	t.Logf("control: truncated artifact decoded to %d nodes against %d written, detector fires",
		len(decoded), len(flat))
}

// TestProbeParseIsNotMemoized guards the measurement itself. If ParseSourceFile cached by
// path or by text, the "parse" timing above would be a cache hit compared against a cache
// hit, and the ratio would be meaningless while looking entirely reasonable.
//
// Two checks: distinct SourceFile pointers per call (no object reuse), and a parse of a
// mutated copy costing the same order as a parse of the original (no text-keyed memo).
func TestProbeParseIsNotMemoized(t *testing.T) {
	sourceText := readSource(t)

	first := parseOnce(sourceText)
	second := parseOnce(sourceText)
	if first == second {
		t.Fatalf("ParseSourceFile returned the same *SourceFile twice; the parse timing is a memo hit, not a parse")
	}

	// A distinct text cannot be served from a text-keyed memo. If it parses in the same
	// ballpark as the original, no memo is involved on either path.
	mutated := sourceText + "\ndeclare const __probe_unique__: number;\n"

	runtime.GC()
	start := time.Now()
	_ = parseOnce(sourceText)
	originalDuration := time.Since(start)

	runtime.GC()
	start = time.Now()
	mutatedFile := parseOnce(mutated)
	mutatedDuration := time.Since(start)

	if mutatedFile == nil {
		t.Fatal("mutated source did not parse")
	}

	ratio := float64(mutatedDuration) / float64(originalDuration)
	if ratio < 0.5 || ratio > 2.0 {
		t.Fatalf("parse of a never-before-seen text took %v against %v for the repeated text (%.2fx); "+
			"that gap is the signature of a memo and the probe's parse baseline cannot be trusted",
			mutatedDuration, originalDuration, ratio)
	}
	t.Logf("no memo: repeated-text parse %v, novel-text parse %v (%.2fx), distinct pointers per call",
		originalDuration, mutatedDuration, ratio)
}

// TestProbeWalkCoversTheTree guards against the other direction of a vacuous measurement:
// a walk that visits a fraction of the tree produces a small artifact that reads back fast
// and proves nothing. Nodes-per-source-byte is the cheap plausibility check.
func TestProbeWalkCoversTheTree(t *testing.T) {
	sourceText := readSource(t)
	flat := flatten(parseOnce(sourceText))

	bytesPerNode := float64(len(sourceText)) / float64(len(flat))

	// TypeScript source averages roughly 10 to 40 source bytes per structural AST node.
	// Far above that band means the walk is skipping subtrees; far below means it is
	// counting something that is not a node. Either way the artifact is not the tree.
	if bytesPerNode < 5 || bytesPerNode > 60 {
		t.Fatalf("%.1f source bytes per node across %d nodes for %d bytes of source; "+
			"the walk is not covering the tree and the artifact does not represent it",
			bytesPerNode, len(flat), len(sourceText))
	}

	// Every node except the root must have a parent inside the array. A dangling parent
	// index means the flatten walk lost its position, which would silently shrink rebuild.
	rootCount := 0
	for index := range flat {
		if flat[index].ParentIndex < 0 {
			rootCount++
			continue
		}
		if int(flat[index].ParentIndex) >= len(flat) {
			t.Fatalf("node %d claims parent %d beyond the %d-node array", index, flat[index].ParentIndex, len(flat))
		}
	}
	if rootCount != 1 {
		t.Fatalf("expected exactly one parentless root, found %d", rootCount)
	}

	t.Logf("walk covers the tree: %d nodes, %.1f source bytes per node, one root, no dangling parents",
		len(flat), bytesPerNode)
}

// TestProbeRealNodeReconstruction is the number that actually decides the domain, and it is
// the one the optimistic rebuild above does not measure.
//
// Rules do not walk a flat array and they do not walk rebuiltNode. They walk *ast.Node, and
// every *ast.Node carries a `data nodeData` interface pointing at a per-kind struct allocated
// from a per-kind arena. A cache that returns anything else has not replaced parsing; it has
// replaced parsing with something the 92 shipped rules cannot consume.
//
// So the honest read cost includes materializing real nodes. This measures the floor of that:
// allocate one node per cached entry through the real factory, set its range and parent. It
// still omits per-kind data fields, identifier text interning, and the arena bookkeeping the
// parser does, so it remains optimistic in the cache's favor. The point is the direction: if
// the floor already eats the margin, the ceiling is worse.
func TestProbeRealNodeReconstruction(t *testing.T) {
	sourceText := readSource(t)
	parsed := parseOnce(sourceText)
	flat := flatten(parsed)
	encoded := encode(flat)

	temporaryPath := t.TempDir() + "/subject.astbin"
	if err := os.WriteFile(temporaryPath, encoded, 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}

	var parseTimings, realReadTimings []time.Duration

	for run := 0; run < runs; run++ {
		runtime.GC()
		start := time.Now()
		if parseOnce(sourceText) == nil {
			t.Fatal("parse returned nil mid-measurement")
		}
		parseTimings = append(parseTimings, time.Since(start))

		runtime.GC()
		start = time.Now()
		raw, err := os.ReadFile(temporaryPath)
		if err != nil {
			t.Fatalf("read artifact: %v", err)
		}
		decoded := decodeFlat(raw)

		// One real ast.Node per cached entry, through the real factory, with the parent link
		// restored. This is the minimum a rule-consumable cache owes.
		factory := ast.NewNodeFactory(ast.NodeFactoryHooks{})
		nodes := make([]*ast.Node, len(decoded))
		for index := range decoded {
			node := factory.NewIdentifier("")
			node.Loc = core.NewTextRange(int(decoded[index].Pos), int(decoded[index].End))
			nodes[index] = node
		}
		for index := range decoded {
			if decoded[index].ParentIndex >= 0 {
				nodes[index].Parent = nodes[decoded[index].ParentIndex]
			}
		}
		realReadTimings = append(realReadTimings, time.Since(start))

		if len(nodes) != len(flat) {
			t.Fatalf("round trip lost nodes: wrote %d, materialized %d", len(flat), len(nodes))
		}
	}

	parseMedian := median(parseTimings)
	realReadMedian := median(realReadTimings)
	ratio := float64(parseMedian) / float64(realReadMedian)

	fmt.Printf("\n=== #5n9as3b, real node reconstruction ===\n")
	fmt.Printf("nodes                        %d\n", len(flat))
	fmt.Printf("parse from source        %8.2f ms\n", float64(parseMedian.Microseconds())/1000)
	fmt.Printf("read + materialize nodes %8.2f ms   %.2fx vs parse\n",
		float64(realReadMedian.Microseconds())/1000, ratio)
	fmt.Printf("\nthis omits per-kind data, identifier text, and arena bookkeeping,\n")
	fmt.Printf("so the real ratio is at or below this figure, never above it.\n\n")
}

// TestProbeAcrossSubjects checks whether the ratio is a property of the mechanism or of
// lib.dom.d.ts specifically. One subject producing 4x proves that subject; three subjects
// of different shape and origin producing the same band is what makes it a finding.
//
// A ratio measured on one file is a sample of one, and this repository has been bitten
// today by exactly that: a rule derived from confirming cases and stated as general.
func TestProbeAcrossSubjects(t *testing.T) {
	subjects := []struct {
		label string
		path  string
	}{
		{"lib.dom.d.ts (2.24 MB, browser DOM declarations)", subjectPath},
		{"csstype index.d.ts (0.85 MB, dense union types)",
			"/Users/kirkouimet/Projects/ahra/node_modules/.pnpm/csstype@3.2.3/node_modules/csstype/index.d.ts"},
		{"@babel/types index.d.ts (0.63 MB, interfaces and generics)",
			"/Users/kirkouimet/Projects/ahra/node_modules/.pnpm/@babel+types@7.29.8/node_modules/@babel/types/lib/index.d.ts"},
		{"aws-sdk client-s3 models_0.d.ts (0.74 MB, generated shapes)",
			"/Users/kirkouimet/Projects/ahra/node_modules/.pnpm/@aws-sdk+client-s3@3.984.0/node_modules/@aws-sdk/client-s3/dist-types/models/models_0.d.ts"},
	}

	fmt.Printf("\n=== #5n9as3b, ratio across subjects ===\n")
	fmt.Printf("%-52s %9s %9s %8s %9s\n", "subject", "nodes", "parse ms", "read ms", "ratio")

	measured := 0
	for _, subject := range subjects {
		bytes, err := os.ReadFile(subject.path)
		if err != nil {
			t.Logf("skipped, not on disk: %s", subject.path)
			continue
		}
		sourceText := string(bytes)

		fileName := tspath.NormalizePath(subject.path)
		parse := func() *ast.SourceFile {
			return parser.ParseSourceFile(ast.SourceFileParseOptions{
				FileName: fileName,
				Path:     tspath.Path(fileName),
			}, sourceText, core.ScriptKindTS)
		}

		parsed := parse()
		if parsed == nil {
			t.Logf("skipped, did not parse: %s", subject.path)
			continue
		}
		flat := flatten(parsed)
		encoded := encode(flat)

		temporaryPath := t.TempDir() + "/subject.astbin"
		if err := os.WriteFile(temporaryPath, encoded, 0o644); err != nil {
			t.Fatalf("write artifact: %v", err)
		}

		var parseTimings, readTimings []time.Duration
		for run := 0; run < runs; run++ {
			runtime.GC()
			start := time.Now()
			parse()
			parseTimings = append(parseTimings, time.Since(start))

			runtime.GC()
			start = time.Now()
			raw, err := os.ReadFile(temporaryPath)
			if err != nil {
				t.Fatalf("read artifact: %v", err)
			}
			rebuild(decodeFlat(raw))
			readTimings = append(readTimings, time.Since(start))
		}

		parseMedian := median(parseTimings)
		readMedian := median(readTimings)
		fmt.Printf("%-52s %9d %9.2f %8.2f %8.2fx\n", subject.label, len(flat),
			float64(parseMedian.Microseconds())/1000,
			float64(readMedian.Microseconds())/1000,
			float64(parseMedian)/float64(readMedian))
		measured++
	}
	fmt.Printf("\n")

	// A table with one row is the sample-of-one this test exists to avoid. Say so rather
	// than printing a tidy result that hides how little it covers.
	if measured < 2 {
		t.Fatalf("only %d subject measured; this test exists to check the ratio across shapes "+
			"and cannot do that with fewer than two", measured)
	}
}

// TestProbeArtifactSizeAgainstSource prices the consequence the ratios above do not carry.
//
// A ratio is per-file and dimensionless. What the domain actually pays is bytes read from
// disk on every warm run, and the flat format is LARGER than the source it replaces: fixed
// 32-byte records against source that averages ~21 bytes per node. And this artifact holds
// no identifier text, no literal values, no comment ranges, none of which a rule-consumable
// cache can omit.
//
// So the cache trades 78.4 MB of declaration text for something bigger, and the read ratio
// has to survive that. This test states the multiplier rather than leaving it implied.
func TestProbeArtifactSizeAgainstSource(t *testing.T) {
	sourceText := readSource(t)
	flat := flatten(parseOnce(sourceText))
	encoded := encode(flat)

	multiplier := float64(len(encoded)) / float64(len(sourceText))

	// node_modules declaration bytes, from the domain brief's measured surface.
	const declarationMegabytes = 78.4

	fmt.Printf("\n=== #5n9as3b, artifact size ===\n")
	fmt.Printf("source                 %.2f MB\n", float64(len(sourceText))/(1024*1024))
	fmt.Printf("artifact               %.2f MB   %.2fx the source it replaces\n",
		float64(len(encoded))/(1024*1024), multiplier)
	fmt.Printf("nodes                  %d\n", len(flat))
	fmt.Printf("\nextrapolated to the %.1f MB of node_modules declarations:\n", declarationMegabytes)
	fmt.Printf("  cache on disk        %.0f MB, structure only\n", declarationMegabytes*multiplier)
	fmt.Printf("  and this artifact holds NO identifier text, literal values, or comment\n")
	fmt.Printf("  ranges, all of which a rule-consumable cache must also carry.\n\n")

	if multiplier <= 1.0 {
		t.Fatalf("artifact measured %.2fx the source, which contradicts a fixed 32-byte record "+
			"against ~21 source bytes per node; the size measurement is wrong", multiplier)
	}
}
