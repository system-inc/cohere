package printing

import (
	"math/rand"
	"testing"
)

// TestFindAncestorAgreesWithAncestors walks every node of generated toy programs, through children slices
// and the label property both, and checks that FindAncestor answers as a search of Ancestors does
// (#vbjv3d6): for a predicate that matches several ancestors, so the nearest must win, one that matches
// only the root, and one that matches nothing.
func TestFindAncestorAgreesWithAncestors(test *testing.T) {
	test.Parallel()
	predicates := map[string]func(*toyNode) bool{
		"list":    func(node *toyNode) bool { return node.Kind == "List" },
		"program": func(node *toyNode) bool { return node.Kind == "Program" },
		"none":    func(*toyNode) bool { return false },
	}
	matched := map[string]int{}
	checked := 0

	var visit func(path *AstPath[*toyNode])
	visit = func(path *AstPath[*toyNode]) {
		for name, predicate := range predicates {
			expected, expectedFound := searchAncestors(path, predicate)
			actual, actualFound := path.FindAncestor(predicate)
			if actual != expected || actualFound != expectedFound {
				test.Fatalf(
					"%s: FindAncestor found %v (%t), a search of Ancestors %v (%t)",
					name,
					actual,
					actualFound,
					expected,
					expectedFound,
				)
			}
			if expectedFound {
				matched[name]++
			}
		}
		checked++
		if node, _ := path.Node(); node.Label != nil {
			Call(path, func(path *AstPath[*toyNode]) struct{} {
				visit(path)
				return struct{}{}
			}, "label")
		}
		path.Each(func(path *AstPath[*toyNode], _ int, _ any) { visit(path) }, "children")
	}
	for seed := int64(1); seed <= 40; seed++ {
		root, _, _ := toyProgram(rand.New(rand.NewSource(seed)))
		visit(NewAstPath(root))
	}

	if matched["list"] == 0 || matched["program"] == 0 || matched["none"] != 0 {
		test.Fatalf(
			"over %d nodes the predicates matched %v; the walk did not reach both a match and none",
			checked,
			matched,
		)
	}
}

// searchAncestors is FindAncestor as it was written before #vbjv3d6: a search of the Ancestors list.
func searchAncestors(path *AstPath[*toyNode], predicate func(*toyNode) bool) (*toyNode, bool) {
	for _, ancestor := range path.Ancestors() {
		if predicate(ancestor) {
			return ancestor, true
		}
	}
	return nil, false
}

// TestPathReadsItsFrames walks a small tree by hand and checks every answer that reads the frames: the key
// and index of a list element, its siblings and neighbours, Match's names and indexes, CallParent,
// CallElement and the stack each restores. The differential never asks most of these, and a stack that
// read one frame off answered the JavaScript oracles identically while it moved 12 files of a real corpus.
func TestPathReadsItsFrames(test *testing.T) {
	test.Parallel()
	first := &toyNode{Kind: "Atom", Value: "a"}
	second := &toyNode{Kind: "Atom", Value: "b"}
	label := &toyNode{Kind: "Atom", Value: "#tag"}
	list := &toyNode{Kind: "List", Label: label, Children: []*toyNode{first, second}}
	program := &toyNode{Kind: "Program", Children: []*toyNode{list}}
	path := NewAstPath(program)

	if key, hasKey := path.Key(); hasKey || key != "" || !path.IsRoot() || path.IsInArray() {
		test.Fatalf("at the root, Key is %q (%t), IsRoot %t, IsInArray %t",
			key, hasKey, path.IsRoot(), path.IsInArray())
	}

	visited := 0
	path.Each(func(path *AstPath[*toyNode], _ int, _ any) {
		path.Each(func(path *AstPath[*toyNode], index int, _ any) {
			if index != 1 {
				return
			}
			visited++
			current, _ := path.Node()
			key, hasKey := path.Key()
			position, hasPosition := path.Index()
			previous, hasPrevious := path.Previous()
			_, hasNext := path.Next()
			parent, _ := path.Parent()
			grandparent, _ := path.Grandparent()
			if current != second || key != "children" || !hasKey || position != 1 || !hasPosition {
				test.Errorf("the second child: node %v, Key %q (%t), Index %d (%t)",
					current, key, hasKey, position, hasPosition)
			}
			if previous != first || !hasPrevious || hasNext || !path.IsLast() || path.IsFirst() {
				test.Errorf("the second child's neighbours: previous %v (%t), next %t, IsLast %t, IsFirst %t",
					previous, hasPrevious, hasNext, path.IsLast(), path.IsFirst())
			}
			if parent != list || grandparent != program || len(path.Siblings().([]*toyNode)) != 2 || path.Siblings().([]*toyNode)[0] != first {
				test.Errorf("the second child's family: parent %v, grandparent %v, siblings %v",
					parent, grandparent, path.Siblings())
			}
			if ancestors := path.Ancestors(); len(ancestors) != 2 || ancestors[0] != list || ancestors[1] != program {
				test.Errorf("the second child's ancestors are %v", ancestors)
			}

			type step struct {
				node     *toyNode
				name     string
				index    int
				hasIndex bool
			}
			var steps []step
			record := func(value any, nameValue any, index int, hasIndex bool) bool {
				node, _ := value.(*toyNode)
				name, _ := nameValue.(string)
				steps = append(steps, step{node: node, name: name, index: index, hasIndex: hasIndex})
				return true
			}
			matched := path.Match(record, record, record)
			want := []step{{second, "", 0, false}, {list, "children", 1, true}, {program, "children", 0, true}}
			if !matched || len(steps) != len(want) {
				test.Fatalf("Match matched %t over %v", matched, steps)
			}
			for stepIndex := range want {
				if steps[stepIndex] != want[stepIndex] {
					test.Errorf("Match step %d is %+v, want %+v", stepIndex, steps[stepIndex], want[stepIndex])
				}
			}
			if path.Match(nil, nil, nil, record) {
				test.Error("Match went past the root")
			}

			atParent := CallParent(path, func(path *AstPath[*toyNode]) *toyNode {
				node, _ := path.Node()
				return node
			}, 0)
			if back, _ := path.Node(); atParent != list || back != second {
				test.Errorf("CallParent ran at %v and left the path at %v", atParent, back)
			}
		}, "children")

		labelKey := Call(path, func(path *AstPath[*toyNode]) string {
			key, _ := path.Key()
			if path.IsInArray() || path.Siblings() != nil {
				test.Error("the label is not in a list, but the path says it is")
			}
			return key
		}, "label")
		element := Call(path, func(path *AstPath[*toyNode]) *toyNode {
			node, _ := path.Node()
			if key, _ := path.Key(); key != "children" {
				test.Errorf("CallElement's element has Key %q", key)
			}
			return node
		}, "children", 0)
		if back, _ := path.Node(); labelKey != "label" || element != first || back != list {
			test.Errorf("Call reached key %q, CallElement %v, and the path is back at %v", labelKey, element, back)
		}
	}, "children")
	if visited != 1 {
		test.Fatalf("the walk reached the second child %d times", visited)
	}
}
