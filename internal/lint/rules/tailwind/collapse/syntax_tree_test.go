package tailwind

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// The fixture is what the shipped Tailwind 4.3.3 engine built and read, captured by
// internal/lint/rules/tailwind/tools/generate_syntax_tree and checked in next to this test.
//
// Measured rather than transcribed, because `getPropertySort` does not do what a reading of it
// suggests. It looks like a tree walk and is a breadth-first queue; it appears to exclude
// `@property` bodies and in fact excludes the `at-root` wrapper they sit inside, with `@property`
// never named. A hand-written expectation set would encode the plausible version of both and agree
// with the engine on nearly every real class.

type astCorpus struct {
	TailwindVersion    string `json:"tailwindVersion"`
	ClassListSize      int    `json:"classListSize"`
	DistinctSignatures int    `json:"distinctSignatures"`
	// FullList records what was measured over the entire class list rather than over the sampled
	// corpus, so the claims in the walk.go doc comment regenerate rather than being remembered.
	FullList struct {
		Compiled                          int `json:"compiled"`
		BreadthFirstDiffersFromDepthFirst int `json:"breadthFirstDiffersFromDepthFirst"`
		WithAtRootSubtree                 int `json:"withAtRootSubtree"`
	} `json:"fullList"`
	Cases          []astCase       `json:"cases"`
	TraversalCases []traversalCase `json:"traversalCases"`
}

// astCase is one compiled utility: the tree the engine built and the reading it took.
type astCase struct {
	ClassName string `json:"className"`
	Signature string `json:"signature"`
	// Node is the declaration list getPropertySort was seeded with: the utility's body before its
	// wrapping rule and before any variant was applied.
	Node *fixtureNode `json:"node"`
	// EmittedNode is the post-variant tree the engine returns. For a duplicating variant such as
	// `not-hover:` it holds more declarations than the reading counts, which is why the reading is
	// taken from Node and never from here.
	EmittedNode *fixtureNode `json:"emittedNode"`
	HasVariants bool         `json:"hasVariants"`

	PropertySort fixtureReading `json:"propertySort"`
}

// traversalCase is a tree where breadth-first and depth-first visit the declarations in different
// orders. These exist because a sorted set of positions usually hides the difference, so a suite
// built only from readings would pass with the wrong traversal.
type traversalCase struct {
	ClassName    string       `json:"className"`
	Node         *fixtureNode `json:"node"`
	BreadthFirst []string     `json:"breadthFirst"`
	DepthFirst   []string     `json:"depthFirst"`
}

type fixtureReading struct {
	Order []int `json:"order"`
	Count int   `json:"count"`
}

// fixtureNode mirrors the engine's node shape as JSON, so that converting it to a Node is an
// explicit step this test controls rather than something struct tags do invisibly.
type fixtureNode struct {
	Kind         string            `json:"kind"`
	Selector     string            `json:"selector"`
	Name         string            `json:"name"`
	Params       string            `json:"params"`
	Property     string            `json:"property"`
	Value        *string           `json:"value"`
	ValuePresent bool              `json:"valuePresent"`
	Important    bool              `json:"important"`
	Context      map[string]string `json:"context"`
	Nodes        []*fixtureNode    `json:"nodes"`
}

func (fixture *fixtureNode) toNode(test *testing.T) *Node {
	test.Helper()

	node := &Node{Kind: NodeKind(fixture.Kind)}
	switch node.Kind {
	case KindRule:
		node.Selector = fixture.Selector
	case KindAtRule:
		node.Name, node.Params = fixture.Name, fixture.Params
	case KindDeclaration:
		node.Property, node.Important, node.ValuePresent = fixture.Property, fixture.Important, fixture.ValuePresent
		if fixture.Value != nil {
			node.Value = *fixture.Value
		}
	case KindComment:
		if fixture.Value != nil {
			node.Value = *fixture.Value
		}
	case KindContext:
		node.Context = fixture.Context
	case KindAtRoot:
	default:
		test.Fatalf("fixture carries an unknown node kind %q, which the port does not model", fixture.Kind)
	}

	for _, child := range fixture.Nodes {
		node.Nodes = append(node.Nodes, child.toNode(test))
	}
	return node
}

func loadASTCorpus(test *testing.T) astCorpus {
	test.Helper()
	raw, readError := os.ReadFile(filepath.Join("testdata", "ast_fixtures.json"))
	if readError != nil {
		test.Fatalf("read fixtures: %v", readError)
	}
	var corpus astCorpus
	if decodeError := json.Unmarshal(raw, &corpus); decodeError != nil {
		test.Fatalf("decode fixtures: %v", decodeError)
	}
	if len(corpus.Cases) == 0 {
		test.Fatal("fixture carries no cases")
	}
	return corpus
}

// TestPropertySortMatchesEngine is the main claim: for every compiled tree the engine built, the Go
// PropertySort produces the reading the engine produced.
func TestPropertySortMatchesEngine(test *testing.T) {
	test.Parallel()
	corpus := loadASTCorpus(test)

	withAtRoot := 0
	withTwSort := 0
	for _, testCase := range corpus.Cases {
		node := testCase.Node.toNode(test)
		got := PropertySort(seedFor(node))

		if got.Count != testCase.PropertySort.Count {
			test.Errorf("%s: count = %d, engine says %d", testCase.ClassName, got.Count, testCase.PropertySort.Count)
		}
		if !equalInts(got.Order, testCase.PropertySort.Order) {
			test.Errorf("%s: order = %v, engine says %v", testCase.ClassName, got.Order, testCase.PropertySort.Order)
		}

		if treeHasKind(node, KindAtRoot) {
			withAtRoot++
		}
		if treeHasProperty(node, "--tw-sort") {
			withTwSort++
		}
	}

	// Report coverage on success. A suite that ran twelve cases and one that ran eleven hundred are
	// indistinguishable from a green line, and the two behaviors that decide this port are carried
	// by subsets of the corpus rather than by all of it.
	test.Logf(
		"tailwind %s: %d compiled trees over %d distinct tree shapes; %d carry an at-root subtree whose declarations must not be counted, %d carry a --tw-sort latch",
		corpus.TailwindVersion, len(corpus.Cases), corpus.DistinctSignatures, withAtRoot, withTwSort,
	)
	if withAtRoot == 0 {
		test.Error("no case in the corpus carries an at-root subtree, so the exclusion that removes @property from the count is untested")
	}
	if withTwSort == 0 {
		test.Error("no case in the corpus carries a --tw-sort declaration, so the latch is untested")
	}
}

// TestPropertySortIsBreadthFirst pins the traversal order itself.
//
// PropertySort's own answers are a sorted set of positions plus a count, both of which are usually
// insensitive to visit order. These cases assert the order directly, using a visitor that records
// what a queue-shaped traversal sees, so a depth-first port fails here even where its readings
// happen to agree.
func TestPropertySortIsBreadthFirst(test *testing.T) {
	test.Parallel()
	corpus := loadASTCorpus(test)
	if len(corpus.TraversalCases) == 0 {
		test.Fatal("fixture carries no traversal-divergence cases, so breadth-first is not actually pinned")
	}

	for _, testCase := range corpus.TraversalCases {
		node := testCase.Node.toNode(test)
		_, got := propertySort(seedFor(node))

		if !equalStrings(got, testCase.BreadthFirst) {
			test.Errorf("%s: visit order = %v, engine's queue visits %v", testCase.ClassName, got, testCase.BreadthFirst)
		}
		if equalStrings(testCase.BreadthFirst, testCase.DepthFirst) {
			test.Errorf("%s: fixture claims divergence but breadth-first and depth-first agree", testCase.ClassName)
		}
		// Guard the guard: if the port were depth-first, this is the answer it would give, and the
		// assertion above must be the thing that rejects it.
		if equalStrings(got, testCase.DepthFirst) {
			test.Errorf("%s: visit order matches depth-first %v, so the traversal is not the engine's", testCase.ClassName, testCase.DepthFirst)
		}
	}

	test.Logf(
		"%d trees where breadth-first and depth-first diverge; across the full %d-candidate class list the engine's own measurement is %d divergent and %d carrying an at-root subtree",
		len(corpus.TraversalCases), corpus.FullList.Compiled,
		corpus.FullList.BreadthFirstDiffersFromDepthFirst, corpus.FullList.WithAtRootSubtree,
	)
}

// TestShadowArbitraryVariableReading pins the single measurement that removed the entire @property
// machinery from this port.
//
// `shadow-[--x]` prints 39 declarations of CSS text and reads {order:[315,316], count:2}. If the
// traversal descended into at-root, or if @property bodies were counted, this count would be far
// larger. It is named explicitly rather than left to the corpus because it is the measurement the
// scope decision rests on, and a scope decision with no test is a comment.
func TestShadowArbitraryVariableReading(test *testing.T) {
	test.Parallel()
	corpus := loadASTCorpus(test)

	found := false
	for _, testCase := range corpus.Cases {
		if testCase.ClassName != "shadow-[--x]" {
			continue
		}
		found = true

		node := testCase.Node.toNode(test)
		got := PropertySort(seedFor(node))

		if got.Count != 2 || !equalInts(got.Order, []int{315, 316}) {
			test.Errorf("shadow-[--x] reads {order:%v count:%d}, want {order:[315 316] count:2}", got.Order, got.Count)
		}
		if !treeHasKind(node, KindAtRoot) {
			test.Error("shadow-[--x] no longer compiles to a tree containing an at-root subtree; the exclusion this case exists to prove is no longer exercised by it")
		}

		declarations := treeCountKind(node, KindDeclaration)
		if declarations <= got.Count {
			test.Errorf("shadow-[--x] holds %d declarations and reads a count of %d; the case is only meaningful while the tree holds many more declarations than the reading counts", declarations, got.Count)
		}
		test.Logf("shadow-[--x]: %d declarations in the tree, reading counts %d", declarations, got.Count)
	}

	if !found {
		test.Fatal("shadow-[--x] is not in the corpus")
	}
}

// TestPropertySortSkipsAbsentValuesButCountsEmptyOnes pins the distinction Go's zero value would
// erase. Upstream the field is `string | undefined`: an absent value is skipped entirely, an empty
// one counts, because `--tw-foo:;` is valid CSS.
func TestPropertySortSkipsAbsentValuesButCountsEmptyOnes(test *testing.T) {
	test.Parallel()
	absent := &Node{Kind: KindDeclaration, Property: "color", Value: "", ValuePresent: false}
	empty := &Node{Kind: KindDeclaration, Property: "color", Value: "", ValuePresent: true}

	if got := PropertySort([]*Node{absent}); got.Count != 0 || len(got.Order) != 0 {
		test.Errorf("absent value: got {order:%v count:%d}, want {order:[] count:0}", got.Order, got.Count)
	}
	if got := PropertySort([]*Node{empty}); got.Count != 1 {
		test.Errorf("empty value: count = %d, want 1", got.Count)
	}
}

// TestPropertySortDoesNotDescendIntoWrappers states the structural rule directly, on a minimal tree
// rather than on a compiled utility, so the reason is visible without reading generated CSS.
func TestPropertySortDoesNotDescendIntoWrappers(test *testing.T) {
	test.Parallel()
	buried := Declaration("display", "flex")

	for _, wrapper := range []struct {
		name string
		node *Node
	}{
		{"at-root", AtRoot(buried)},
		{"context", Context(map[string]string{"theme": "true"}, buried)},
	} {
		if got := PropertySort([]*Node{wrapper.node}); got.Count != 0 {
			test.Errorf("%s: count = %d, want 0; the reading must not descend into it", wrapper.name, got.Count)
		}
	}

	for _, container := range []struct {
		name string
		node *Node
	}{
		{"rule", StyleRule(".x", buried)},
		{"at-rule", AtRule("@media", "print", buried)},
	} {
		if got := PropertySort([]*Node{container.node}); got.Count != 1 {
			test.Errorf("%s: count = %d, want 1; the reading must descend into it", container.name, got.Count)
		}
	}
}

// TestPropertySortTwSortLatch states the three behaviors of the --tw-sort override in isolation.
func TestPropertySortTwSortLatch(test *testing.T) {
	test.Parallel()
	rowGap, ok := PropertyOrder["row-gap"]
	if !ok {
		test.Fatal("row-gap is missing from PropertyOrder")
	}
	display, ok := PropertyOrder["display"]
	if !ok {
		test.Fatal("display is missing from PropertyOrder")
	}

	test.Run("latches and suppresses later positions", func(test *testing.T) {
		test.Parallel()
		got := PropertySort([]*Node{
			Declaration("--tw-sort", "row-gap"),
			Declaration("display", "flex"),
		})
		if !equalInts(got.Order, []int{rowGap}) {
			test.Errorf("order = %v, want [%d]; display must not contribute after the latch", got.Order, rowGap)
		}
		if got.Count != 2 {
			test.Errorf("count = %d, want 2; the latch suppresses order, never the count", got.Count)
		}
	})

	test.Run("positions before the latch still count", func(test *testing.T) {
		test.Parallel()
		got := PropertySort([]*Node{
			Declaration("display", "flex"),
			Declaration("--tw-sort", "row-gap"),
		})
		if !equalInts(got.Order, []int{display, rowGap}) && !equalInts(got.Order, []int{rowGap, display}) {
			test.Errorf("order = %v, want both %d and %d", got.Order, display, rowGap)
		}
	})

	test.Run("an unknown value does not latch", func(test *testing.T) {
		test.Parallel()
		got := PropertySort([]*Node{
			Declaration("--tw-sort", "not-a-real-property"),
			Declaration("display", "flex"),
		})
		if !equalInts(got.Order, []int{display}) {
			test.Errorf("order = %v, want [%d]; an unrecognized --tw-sort must not latch", got.Order, display)
		}
	})
}

// TestPropertySortOrderIsAscendingAndDeduplicated pins the shape of Order over the whole corpus,
// which the per-case comparison would satisfy vacuously if the engine's own answers happened to be
// already sorted.
func TestPropertySortOrderIsAscendingAndDeduplicated(test *testing.T) {
	test.Parallel()
	corpus := loadASTCorpus(test)

	for _, testCase := range corpus.Cases {
		node := testCase.Node.toNode(test)
		got := PropertySort(seedFor(node))
		for index := 1; index < len(got.Order); index++ {
			if got.Order[index] <= got.Order[index-1] {
				test.Fatalf("%s: order %v is not strictly ascending at %d", testCase.ClassName, got.Order, index)
			}
		}
	}
}

// TestVariantsDoNotChangeTheReading pins the contract that made the fixture's shape necessary.
//
// The engine takes a candidate's reading from its body before any variant is applied, so
// `hover:flex` and `flex` read alike even though `not-hover:flex` compiles to twice the
// declarations. The fixture records both trees; this asserts the emitted tree really can differ
// while the reading does not, so the invariance is demonstrated rather than assumed.
func TestVariantsDoNotChangeTheReading(test *testing.T) {
	test.Parallel()
	corpus := loadASTCorpus(test)

	withVariants := 0
	whereEmittedDiffers := 0

	for _, testCase := range corpus.Cases {
		if !testCase.HasVariants || testCase.EmittedNode == nil {
			continue
		}
		withVariants++

		read := testCase.Node.toNode(test)
		emitted := testCase.EmittedNode.toNode(test)

		if treeCountKind(emitted, KindDeclaration) != treeCountKind(read, KindDeclaration) {
			whereEmittedDiffers++
		}

		// The reading is the engine's, taken from the pre-variant body. The post-variant tree is
		// never the input.
		if got := PropertySort(seedFor(read)); got.Count != testCase.PropertySort.Count {
			test.Errorf("%s: count = %d, engine says %d", testCase.ClassName, got.Count, testCase.PropertySort.Count)
		}
	}

	if withVariants == 0 {
		test.Fatal("no case in the corpus carries a variant, so variant-invariance is untested")
	}
	if whereEmittedDiffers == 0 {
		test.Error("no variant case emits a different declaration count than it reads, so this test cannot distinguish the pre-variant body from the emitted tree")
	}
	test.Logf("%d variant-carrying cases, %d of which emit a different declaration count than their reading counts", withVariants, whereEmittedDiffers)
}

// TestPropertySortDoesNotMutateItsInput pins that the traversal leaves the caller's slice alone.
//
// The queue is seeded with a copy. Seeding it with the slice itself passes every other test in this
// file and is still wrong: the queue grows by append, so once it outgrows the caller's backing array
// it detaches, but until then it writes children over whatever followed the caller's slice in that
// array. That is a use-after-free-shaped bug in Go's clothing, and it only shows up when the caller
// passes a subslice of something larger, which is exactly what the CSS parser will do.
func TestPropertySortDoesNotMutateItsInput(test *testing.T) {
	test.Parallel()
	// A backing array larger than the slice handed to PropertySort, so an append that fails to copy
	// writes into the tail rather than reallocating.
	backing := make([]*Node, 0, 16)
	backing = append(backing,
		StyleRule(".a", Declaration("display", "flex"), Declaration("color", "red")),
	)
	tail := []*Node{Declaration("margin", "0"), Declaration("padding", "0")}
	backing = append(backing, tail...)

	seed := backing[:1]

	before := make([]*Node, len(backing))
	copy(before, backing)

	PropertySort(seed)

	for index := range backing {
		if backing[index] != before[index] {
			test.Fatalf("PropertySort overwrote the caller's backing array at index %d; it must seed its queue with a copy", index)
		}
	}
	if len(seed) != 1 {
		test.Errorf("seed length = %d, want 1", len(seed))
	}
}

// seedFor recovers the declaration list the engine seeded getPropertySort with.
//
// The fixture stores that list already wrapped in the candidate's `.selector` rule, because that is
// the shape compileAstNodes returns. Unwrapping one level asks the same question the engine asked.
func seedFor(node *Node) []*Node {
	if node.Kind == KindRule {
		return node.Nodes
	}
	return []*Node{node}
}

func treeHasKind(node *Node, kind NodeKind) bool {
	found := false
	Walk([]*Node{node}, func(current *Node) WalkAction {
		if current.Kind == kind {
			found = true
			return WalkStop
		}
		return WalkContinue
	})
	return found
}

func treeHasProperty(node *Node, property string) bool {
	found := false
	Walk([]*Node{node}, func(current *Node) WalkAction {
		if current.Kind == KindDeclaration && current.Property == property {
			found = true
			return WalkStop
		}
		return WalkContinue
	})
	return found
}

func treeCountKind(node *Node, kind NodeKind) int {
	count := 0
	Walk([]*Node{node}, func(current *Node) WalkAction {
		if current.Kind == kind {
			count++
		}
		return WalkContinue
	})
	return count
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for index := range a {
		if a[index] != b[index] {
			return false
		}
	}
	return true
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for index := range a {
		if a[index] != b[index] {
			return false
		}
	}
	return true
}
