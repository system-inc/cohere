package nexus

import (
	"path"
	"strings"

	"github.com/microsoft/TypeScript/tsc/shim/tspath"
)

// correctnessNoImportCycleLoadTimeReadGraph is the program's runtime import graph, kept only for the
// files inside a cycle.
type correctnessNoImportCycleLoadTimeReadGraph struct {
	// component numbers each file's strongly connected component. A file in no cycle is absent.
	component map[tspath.PathKey]int
	// edges are the runtime edges between files of the same component, for naming the cycle.
	edges map[tspath.PathKey][]tspath.PathKey
	// fileNames spells each path the way the program does, for the message.
	fileNames map[tspath.PathKey]string
}

// cycleThrough names one cycle through both files, `A.ts → B.ts → A.ts`, from the shortest path each
// way between them.
func (graph *correctnessNoImportCycleLoadTimeReadGraph) cycleThrough(
	reader tspath.PathKey,
	declaring tspath.PathKey,
) string {
	there := graph.shortestPath(reader, declaring)
	back := graph.shortestPath(declaring, reader)
	if len(there) == 0 || len(back) == 0 {
		return "an import cycle"
	}
	var names []string
	for _, step := range append(there, back[1:]...) {
		names = append(names, path.Base(graph.fileNames[step]))
	}
	return strings.Join(names, " → ")
}

// shortestPath returns the files from one to the other along runtime edges, both ends included.
func (graph *correctnessNoImportCycleLoadTimeReadGraph) shortestPath(
	from tspath.PathKey,
	to tspath.PathKey,
) []tspath.PathKey {
	previous := map[tspath.PathKey]tspath.PathKey{from: from}
	queue := []tspath.PathKey{from}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if current == to {
			var steps []tspath.PathKey
			for step := to; step != from; step = previous[step] {
				steps = append(steps, step)
			}
			steps = append(steps, from)
			for left, right := 0, len(steps)-1; left < right; left, right = left+1, right-1 {
				steps[left], steps[right] = steps[right], steps[left]
			}
			return steps
		}
		for _, next := range graph.edges[current] {
			if _, seen := previous[next]; !seen {
				previous[next] = current
				queue = append(queue, next)
			}
		}
	}
	return nil
}
