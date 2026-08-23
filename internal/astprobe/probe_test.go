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
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"strings"
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

// #nk6hmwp: the 4x in TestProbeAcrossSubjects was measured on an artifact carrying no
// identifier text, and no rule can run against that. `no-debugger` needs to know the token
// says "debugger"; every naming rule needs the spelling; every import rule needs the
// specifier. So that ratio was the read speed of a cache nobody can consume.
//
// This measures the same read WITH text, which is the largest single omission. The format
// question is settled and not reopened: fixed records, little-endian, parent indices. The
// question is what the artifact must contain.

// textOfNode returns a node's text, or "" for the kinds that carry none.
//
// Node.Text() panics rather than returning empty on an unhandled kind (ast.go:307), and most
// nodes are unhandled kinds — a SourceFile, a Block, a TypeReference. So the walk has to ask
// only the kinds that answer. This list mirrors the switch in Node.Text; if upstream adds a
// text-bearing kind, this measurement silently under-counts text rather than crashing, which
// is worth knowing when the number is re-taken after a bump.
func textOfNode(node *ast.Node) string {
	switch node.Kind {
	case ast.KindIdentifier, ast.KindPrivateIdentifier,
		ast.KindStringLiteral, ast.KindNumericLiteral, ast.KindBigIntLiteral,
		ast.KindNoSubstitutionTemplateLiteral,
		ast.KindTemplateHead, ast.KindTemplateMiddle, ast.KindTemplateTail,
		ast.KindRegularExpressionLiteral,
		ast.KindJsxNamespacedName, ast.KindMetaProperty,
		ast.KindJSDocText, ast.KindJSDocLink, ast.KindJSDocLinkCode, ast.KindJSDocLinkPlain:
		return node.Text()
	}
	return ""
}

// textNode extends flatNode with a slot into a string table. Two int32 rather than a Go
// string, because a string header is a pointer and pointers are what the flat layout exists
// to avoid.
type textNode struct {
	Kind        int32
	Flags       int32
	Pos         int32
	End         int32
	ParentIndex int32
	TextOffset  int32 // byte offset into the string blob, -1 when the node carries no text
	TextLength  int32
	_           int32
}

const textNodeSize = 32

// flattenWithText walks the tree recording text for every node that carries any.
//
// The string table is interned: a name appearing a thousand times is stored once. That is
// what a real cache would do and it is the variant most favorable to the cache, so if this
// loses, the naive variant loses harder. The naive one is measured too, below, because the
// gap between them is the value of interning and it is worth stating rather than assuming.
func flattenWithText(sourceFile *ast.SourceFile, intern bool) ([]textNode, []byte) {
	nodes := make([]textNode, 0, 512*1024)
	var blob []byte
	table := make(map[string]int32)

	put := func(text string) (int32, int32) {
		if text == "" {
			return -1, 0
		}
		if intern {
			if offset, seen := table[text]; seen {
				return offset, int32(len(text))
			}
		}
		offset := int32(len(blob))
		blob = append(blob, text...)
		if intern {
			table[text] = offset
		}
		return offset, int32(len(text))
	}

	var visit func(node *ast.Node, parentIndex int32)
	visit = func(node *ast.Node, parentIndex int32) {
		selfIndex := int32(len(nodes))
		offset, length := put(textOfNode(node))
		nodes = append(nodes, textNode{
			Kind:        int32(node.Kind),
			Flags:       int32(node.Flags),
			Pos:         int32(node.Pos()),
			End:         int32(node.End()),
			ParentIndex: parentIndex,
			TextOffset:  offset,
			TextLength:  length,
		})
		node.ForEachChild(func(child *ast.Node) bool {
			visit(child, selfIndex)
			return false
		})
	}

	visit(sourceFile.AsNode(), -1)
	return nodes, blob
}

func encodeWithText(nodes []textNode, blob []byte) []byte {
	// 8 bytes of header carrying the node count, so the reader can split the two regions
	// without a separate index file.
	buffer := make([]byte, 8+len(nodes)*textNodeSize+len(blob))
	binary.LittleEndian.PutUint32(buffer[0:], uint32(len(nodes)))
	binary.LittleEndian.PutUint32(buffer[4:], uint32(len(blob)))
	base := 8
	for index, node := range nodes {
		offset := base + index*textNodeSize
		binary.LittleEndian.PutUint32(buffer[offset+0:], uint32(node.Kind))
		binary.LittleEndian.PutUint32(buffer[offset+4:], uint32(node.Flags))
		binary.LittleEndian.PutUint32(buffer[offset+8:], uint32(node.Pos))
		binary.LittleEndian.PutUint32(buffer[offset+12:], uint32(node.End))
		binary.LittleEndian.PutUint32(buffer[offset+16:], uint32(node.ParentIndex))
		binary.LittleEndian.PutUint32(buffer[offset+20:], uint32(node.TextOffset))
		binary.LittleEndian.PutUint32(buffer[offset+24:], uint32(node.TextLength))
	}
	copy(buffer[base+len(nodes)*textNodeSize:], blob)
	return buffer
}

// readWithText is the honest read: materialize nodes AND hand each one its text as a Go
// string. The strings are sliced out of the blob rather than copied, which is the fastest
// correct thing available — a rule comparing node.Text() == "debugger" needs a string, and
// this produces one without allocating a new backing array.
func readWithText(buffer []byte) ([]rebuiltNode, []string) {
	nodeCount := int(binary.LittleEndian.Uint32(buffer[0:]))
	blobLength := int(binary.LittleEndian.Uint32(buffer[4:]))
	base := 8
	blobStart := base + nodeCount*textNodeSize
	blob := buffer[blobStart : blobStart+blobLength]

	nodes := make([]rebuiltNode, nodeCount)
	texts := make([]string, nodeCount)
	for index := range nodes {
		offset := base + index*textNodeSize
		nodes[index].Kind = int32(binary.LittleEndian.Uint32(buffer[offset+0:]))
		nodes[index].Flags = int32(binary.LittleEndian.Uint32(buffer[offset+4:]))
		nodes[index].Pos = int32(binary.LittleEndian.Uint32(buffer[offset+8:]))
		nodes[index].End = int32(binary.LittleEndian.Uint32(buffer[offset+12:]))
		parentIndex := int32(binary.LittleEndian.Uint32(buffer[offset+16:]))
		textOffset := int32(binary.LittleEndian.Uint32(buffer[offset+20:]))
		textLength := int32(binary.LittleEndian.Uint32(buffer[offset+24:]))
		if parentIndex >= 0 {
			parent := &nodes[parentIndex]
			nodes[index].Parent = parent
			parent.Children = append(parent.Children, &nodes[index])
		}
		if textOffset >= 0 {
			texts[index] = string(blob[textOffset : textOffset+textLength])
		}
	}
	return nodes, texts
}

func TestProbeReadWithIdentifierText(t *testing.T) {
	subjects := []struct {
		label string
		path  string
	}{
		{"lib.dom.d.ts", subjectPath},
		{"csstype", "/Users/kirkouimet/Projects/ahra/node_modules/.pnpm/csstype@3.2.3/node_modules/csstype/index.d.ts"},
		{"@babel/types", "/Users/kirkouimet/Projects/ahra/node_modules/.pnpm/@babel+types@7.29.8/node_modules/@babel/types/lib/index.d.ts"},
		{"aws-sdk models_0", "/Users/kirkouimet/Projects/ahra/node_modules/.pnpm/@aws-sdk+client-s3@3.984.0/node_modules/@aws-sdk/client-s3/dist-types/models/models_0.d.ts"},
	}

	fmt.Printf("\n=== #nk6hmwp: read WITH identifier text ===\n")
	fmt.Printf("%-18s %8s %9s %10s %9s %8s %8s\n",
		"subject", "nodes", "withText", "artifact", "parse ms", "read ms", "ratio")

	measured := 0
	for _, subject := range subjects {
		raw, err := os.ReadFile(subject.path)
		if err != nil {
			t.Logf("skipped, not on disk: %s", subject.path)
			continue
		}
		sourceText := string(raw)
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

		nodes, blob := flattenWithText(parsed, true)
		encoded := encodeWithText(nodes, blob)

		withText := 0
		for index := range nodes {
			if nodes[index].TextOffset >= 0 {
				withText++
			}
		}

		// Prove the input is real. An artifact whose string blob is empty would read back
		// beautifully and mean nothing, which is exactly the failure this measurement exists
		// to correct in the first place.
		if withText == 0 || len(blob) == 0 {
			t.Fatalf("%s: %d nodes carry text and the blob is %d bytes; "+
				"this is measuring a textless artifact again", subject.label, withText, len(blob))
		}

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
			fileBytes, err := os.ReadFile(temporaryPath)
			if err != nil {
				t.Fatalf("read artifact: %v", err)
			}
			gotNodes, gotTexts := readWithText(fileBytes)
			readTimings = append(readTimings, time.Since(start))

			if len(gotNodes) != len(nodes) || len(gotTexts) != len(nodes) {
				t.Fatalf("round trip lost nodes: wrote %d, read %d", len(nodes), len(gotNodes))
			}
		}

		parseMedian := median(parseTimings)
		readMedian := median(readTimings)
		fmt.Printf("%-18s %8d %9d %9.2fMB %9.2f %8.2f %7.2fx\n",
			subject.label, len(nodes), withText,
			float64(len(encoded))/(1024*1024),
			float64(parseMedian.Microseconds())/1000,
			float64(readMedian.Microseconds())/1000,
			float64(parseMedian)/float64(readMedian))
		measured++
	}
	fmt.Printf("\n")

	if measured < 2 {
		t.Fatalf("only %d subject measured; the ratio needs more than one shape", measured)
	}
}

// TestProbeTextRoundTripIsCorrect proves the text actually survives, which the timing test
// cannot show. A read that returned empty strings for every node would be fast and wrong, and
// it would look identical to a fast correct one in a table of milliseconds.
func TestProbeTextRoundTripIsCorrect(t *testing.T) {
	sourceText := readSource(t)
	parsed := parseOnce(sourceText)

	nodes, blob := flattenWithText(parsed, true)
	encoded := encodeWithText(nodes, blob)
	_, texts := readWithText(encoded)

	// Walk the live tree again and compare every text-bearing node against what came back.
	index := 0
	mismatches := 0
	checked := 0
	var visit func(node *ast.Node)
	visit = func(node *ast.Node) {
		self := index
		index++
		if expected := textOfNode(node); expected != "" {
			checked++
			if texts[self] != expected {
				if mismatches < 5 {
					t.Errorf("node %d kind %v: wrote %q, read %q", self, node.Kind, expected, texts[self])
				}
				mismatches++
			}
		}
		node.ForEachChild(func(child *ast.Node) bool {
			visit(child)
			return false
		})
	}
	visit(parsed.AsNode())

	if checked == 0 {
		t.Fatal("zero text-bearing nodes checked; this test cannot detect a text failure")
	}
	if mismatches > 0 {
		t.Fatalf("%d of %d text-bearing nodes round-tripped wrong", mismatches, checked)
	}
	t.Logf("text round trip exact across %d text-bearing nodes of %d total", checked, index)
}

// TestProbeTextDetectorCanFail proves the comparison above can actually fail. A corrupted blob
// must produce mismatches; if it does not, the check is vacuous and its clean result means
// nothing.
func TestProbeTextDetectorCanFail(t *testing.T) {
	sourceText := readSource(t)
	parsed := parseOnce(sourceText)
	nodes, blob := flattenWithText(parsed, true)

	if len(blob) == 0 {
		t.Fatal("empty blob; nothing to corrupt")
	}
	corrupted := append([]byte(nil), blob...)
	for i := range corrupted {
		corrupted[i] = 'X'
	}

	encoded := encodeWithText(nodes, corrupted)
	_, texts := readWithText(encoded)

	differing := 0
	for index := range nodes {
		if nodes[index].TextOffset >= 0 {
			original := string(blob[nodes[index].TextOffset : nodes[index].TextOffset+nodes[index].TextLength])
			if texts[index] != original {
				differing++
			}
		}
	}
	if differing == 0 {
		t.Fatal("a fully corrupted string blob produced identical text; " +
			"the text comparison cannot detect corruption and every clean result from it is vacuous")
	}
	t.Logf("control: corrupting the blob changed %d texts, detector fires", differing)
}

// TestProbeGraphSplit answers the question every saving estimate on #8fbvwnz currently hedges:
// the graph phase is parse PLUS module resolution, and only the parse half is cacheable by an
// AST cache. Every figure I have reported so far said "parse is 60 to 80% of graph" as a band
// because nobody had split it.
//
// The method: parse every file the program contains, standalone, with no resolution and no
// program construction, and compare that total against the measured graph phase. Parsing is
// the one phase that depends on exactly one file's bytes, so parsing every file in isolation
// is a faithful lower bound on the parse share.
//
// This is a lower bound rather than an exact split, and the reason is worth stating: the real
// program construction also parses, but it does so interleaved with resolution and across
// several goroutines, so wall-clock attribution inside it is not separable without upstream
// instrumentation. What this measures is "what does parsing all of it cost on its own", which
// is exactly the quantity an AST cache replaces.
func TestProbeGraphSplit(t *testing.T) {
	// The file list comes from the buildinfo, which names every file the program contains.
	// Reading it is cheaper than constructing a program here, and it is the same population:
	// fileNames is 9,982 on both artifacts I have inspected.
	buildInfoPath := "/Users/kirkouimet/Projects/ahra/.cache/ts/tsconfig.tsbuildinfo"
	raw, err := os.ReadFile(buildInfoPath)
	if err != nil {
		t.Skipf("no buildinfo to enumerate the program: %v", err)
	}

	var buildInfo struct {
		FileNames []string `json:"fileNames"`
	}
	if err := json.Unmarshal(raw, &buildInfo); err != nil {
		t.Fatalf("parse buildinfo: %v", err)
	}
	if len(buildInfo.FileNames) < 1000 {
		t.Fatalf("buildinfo named %d files; that is not this program and the split would be "+
			"measured against the wrong population", len(buildInfo.FileNames))
	}

	// Paths in the buildinfo are relative to its own directory.
	buildInfoDirectory := "/Users/kirkouimet/Projects/ahra/.cache/ts"

	type loaded struct {
		path string
		text string
	}
	files := make([]loaded, 0, len(buildInfo.FileNames))
	totalBytes := 0
	missing := 0
	for _, name := range buildInfo.FileNames {
		path := name
		if !strings.HasPrefix(path, "/") {
			path = tspath.NormalizePath(buildInfoDirectory + "/" + name)
		}
		content, err := os.ReadFile(path)
		if err != nil {
			missing++
			continue
		}
		files = append(files, loaded{path: path, text: string(content)})
		totalBytes += len(content)
	}

	// A split measured over a fraction of the program is not the split. Say so rather than
	// printing a confident number for the wrong population.
	if len(files) < len(buildInfo.FileNames)*9/10 {
		t.Fatalf("only %d of %d files readable; the parse share would be measured against a "+
			"different program than the one graph builds", len(files), len(buildInfo.FileNames))
	}

	// Read all files first, above, so this measures parsing rather than disk. An AST cache
	// replaces the parse, not the read, and conflating them would inflate the saving.
	const splitRuns = 3
	var parseTimings []time.Duration
	for run := 0; run < splitRuns; run++ {
		runtime.GC()
		start := time.Now()
		for _, file := range files {
			fileName := tspath.NormalizePath(file.path)
			kind := core.ScriptKindTS
			if strings.HasSuffix(fileName, ".tsx") {
				kind = core.ScriptKindTSX
			} else if strings.HasSuffix(fileName, ".js") || strings.HasSuffix(fileName, ".mjs") {
				kind = core.ScriptKindJS
			} else if strings.HasSuffix(fileName, ".jsx") {
				kind = core.ScriptKindJSX
			} else if strings.HasSuffix(fileName, ".json") {
				kind = core.ScriptKindJSON
			}
			parser.ParseSourceFile(ast.SourceFileParseOptions{
				FileName: fileName,
				Path:     tspath.Path(fileName),
			}, file.text, kind)
		}
		parseTimings = append(parseTimings, time.Since(start))
	}

	parseMedian := median(parseTimings)

	// The graph phase measured on this tree, five runs of `verify --no-fix`, median. Stated as
	// a constant with its provenance rather than re-measured here, because running the full
	// binary from a test would measure a different process under different load.
	const measuredGraphMilliseconds = 528.0
	parseMilliseconds := float64(parseMedian.Microseconds()) / 1000

	fmt.Printf("\n=== #8fbvwnz: what fraction of graph is parse ===\n")
	fmt.Printf("files parsed          %d of %d named (%d unreadable)\n",
		len(files), len(buildInfo.FileNames), missing)
	fmt.Printf("source                %.1f MB\n", float64(totalBytes)/(1024*1024))
	fmt.Printf("parse, standalone     %.0f ms   median of %d\n", parseMilliseconds, splitRuns)
	fmt.Printf("graph phase           %.0f ms   measured, verify --no-fix, median of 5\n",
		measuredGraphMilliseconds)
	fmt.Printf("parse share           %.0f%% of graph\n",
		100*parseMilliseconds/measuredGraphMilliseconds)
	fmt.Printf("\nsingle-threaded here against a parallel program construction, so this is an\n")
	fmt.Printf("UPPER bound on parse time and the share is indicative rather than exact.\n\n")

	for _, ratio := range []float64{2.25, 3.0, 5.2} {
		saved := parseMilliseconds - parseMilliseconds/ratio
		fmt.Printf("  at %.2fx read: parse %.0fms becomes %.0fms, saves %.0fms\n",
			ratio, parseMilliseconds, parseMilliseconds/ratio, saved)
	}
	fmt.Printf("\n")
}
