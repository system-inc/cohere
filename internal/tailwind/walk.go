// Traversals over the CSS AST: the general depth-first Walk, and the breadth-first PropertySort
// that produces a candidate's sort key.
//
// Ported from `src/walk.ts` and the `getPropertySort` half of `src/compile.ts` at Tailwind 4.3.3.
//
// The two traversals in this file visit in different orders, and that is faithful rather than
// sloppy. Upstream `walk` is depth-first; upstream `getPropertySort` does not call `walk` at all,
// it runs its own queue. Unifying them would be the tidier-looking port and would silently change
// answers — see PropertySort.
package tailwind

import "slices"

// WalkAction is what a visitor returns to steer the traversal.
type WalkAction int

const (
	// WalkContinue descends into the node's children and then proceeds to its next sibling.
	WalkContinue WalkAction = iota
	// WalkSkip proceeds to the node's next sibling without descending into its children.
	WalkSkip
	// WalkStop ends the traversal immediately. No further node is visited.
	WalkStop
)

// Walk visits nodes depth-first, in document order, calling visit on each.
//
// The visitor's WalkAction steers the traversal: WalkSkip prunes the current node's subtree,
// WalkStop ends the whole walk. A visitor that always returns WalkContinue sees every node.
//
// This is the enter-only half of upstream's walk. The exit hook and the Replace family of actions
// are not ported; see the boundary note in ast.go. Because there is no mutation path, the tree must
// not be modified during a walk.
//
// Descent is by IsContainer, so KindContext and KindAtRoot subtrees are visited. This is upstream's
// behavior — its walk descends on the presence of a `nodes` array, not on a kind allowlist — and it
// is deliberately *not* what PropertySort does.
func Walk(nodes []*Node, visit func(node *Node) WalkAction) {
	walkNodes(nodes, visit)
}

// walkNodes returns false once the traversal has been stopped, so that every enclosing level
// unwinds without visiting another sibling.
func walkNodes(nodes []*Node, visit func(node *Node) WalkAction) bool {
	for _, node := range nodes {
		switch visit(node) {
		case WalkStop:
			return false
		case WalkSkip:
			continue
		case WalkContinue:
			if node.IsContainer() && len(node.Nodes) > 0 {
				if !walkNodes(node.Nodes, visit) {
					return false
				}
			}
		}
	}
	return true
}

// Sort is the reading of a compiled utility: the sort key that
// enforce-consistent-class-order compares.
//
// Order holds the ascending, deduplicated positions in PropertyOrder of the properties the utility
// declares. Count is how many declarations it has, which breaks ties between utilities touching the
// same properties.
type Sort struct {
	Order []int
	Count int
}

// PropertySort computes the reading of a compiled utility from its declaration list.
//
// Ported from `getPropertySort` in `src/compile.ts`. Three details decide correctness, and each was
// measured against the shipped 4.3.3 engine rather than inferred from the source.
//
// # It is breadth-first, and that is observable
//
// Upstream is a queue — shift from the front, push children to the back — not a call to `walk`.
// Substituting a depth-first traversal changes the visit order on 32 of the 23,286 candidates in
// Tailwind's own class list (measured across the full list, all `bg-linear`/`bg-radial`/`bg-conic`
// shapes, where a declaration sits beside an `@supports` block that redeclares it). Order is a
// sorted set, so a reordering alone often cancels out; the latch below is what makes it visible,
// because a `--tw-sort` reached earlier suppresses positions a later one would have contributed.
// Depth-first would therefore be right on 99.86% of real input, which is the failure mode that
// hides for a release rather than the one that shows up in a test.
//
// # Only rule and at-rule are descended into
//
// KindContext and KindAtRoot hold children and are skipped, matching upstream's explicit
// `node.kind === 'rule' || node.kind === 'at-rule'`. This is the structural reason `@property`
// bodies are not counted: the compiler emits them inside an at-root wrapper, so the whole subtree
// is invisible here without `@property` being special-cased anywhere. Measured: `-space-x-4`
// compiles to a tree containing three `@property` declarations under an at-root and reads
// {order:[132], count:4}, counting only the four declarations in its inner rule. 10,160 of the
// 23,286 candidates carry an at-root subtree. Descending into at-root would inflate the count on
// all of them, and count is half the sort key.
//
// # The --tw-sort latch is one-way and only gates Order
//
// A utility can declare `--tw-sort: <property>` to sort at another property's position. The first
// such declaration whose value is a known property wins; from then on no further declaration
// contributes to Order. Count keeps incrementing regardless. A `--tw-sort` whose value is unknown
// does not latch, and falls through to be looked up as a property name in its own right.
//
// The declaration list passed here is the utility's body before it is wrapped in its `.selector`
// rule and before any variant is applied, matching upstream's call site. Variants never change a
// candidate's reading.
func PropertySort(nodes []*Node) Sort {
	sort := Sort{}

	// seen keeps Order deduplicated while positions are collected out of ascending order; upstream
	// uses a Set and sorts at the end.
	seen := map[int]bool{}
	seenTwSort := false

	// A queue, matching upstream's `q.shift()`. The head index avoids re-slicing on every step.
	queue := make([]*Node, len(nodes))
	copy(queue, nodes)

	for head := 0; head < len(queue); head++ {
		node := queue[head]

		switch node.Kind {
		case KindDeclaration:
			// An absent value is skipped entirely and does not count. An empty value does count:
			// `--tw-foo:;` is valid CSS.
			if !node.ValuePresent {
				continue
			}

			sort.Count++

			if seenTwSort {
				continue
			}

			if node.Property == "--tw-sort" {
				if position, ok := PropertyOrder[node.Value]; ok {
					if !seen[position] {
						seen[position] = true
						sort.Order = append(sort.Order, position)
					}
					seenTwSort = true
					continue
				}
				// An unrecognized --tw-sort value does not latch, and falls through to the lookup
				// below, where `--tw-sort` itself is not a known property and contributes nothing.
			}

			if position, ok := PropertyOrder[node.Property]; ok {
				if !seen[position] {
					seen[position] = true
					sort.Order = append(sort.Order, position)
				}
			}

		case KindRule, KindAtRule:
			queue = append(queue, node.Nodes...)
		}
	}

	slices.Sort(sort.Order)
	return sort
}
