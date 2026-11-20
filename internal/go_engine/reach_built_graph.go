package go_engine

import (
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/load"
)

// builtLoadMode is how a build's load holds a package of its graph.
type builtLoadMode string

const (
	// builtFromSource is a package the build type-checked from its source.
	builtFromSource builtLoadMode = "FromSource"
	// builtRestored is a package the build did not check from source: one whose types it read from export data, the
	// findings store's or the system cache's, and one it restored and never read the types of, nothing it checked
	// importing it. The two are one mode, since nothing reads them apart (checkedFromSource).
	builtRestored builtLoadMode = "Restored"
)

// reachBuiltGraph is what the load mode rule reads of a build's whole graph (#wzre5eg): how the build holds each
// package, by its ID; the IDs it holds of each path, since a package and its test variants share one; and every path
// an import spec names in a file of a package it checked from source, read from the syntax and never from the graph's
// own edges, so the two can be held against each other (bodyReacher.startsCarry).
type reachBuiltGraph struct {
	modes       map[string]builtLoadMode
	identifiers map[string][]string
	imported    map[string]bool
}

// newReachBuiltGraph is the graph of loaded, every package its roots reach, read before the build's rules let its
// syntax go.
func newReachBuiltGraph(loaded *load.Loaded) *reachBuiltGraph {
	graph := &reachBuiltGraph{modes: map[string]builtLoadMode{}, identifiers: map[string][]string{},
		imported: map[string]bool{}}
	packages.Visit(loaded.Roots, nil, func(each *packages.Package) {
		if each.TypesInfo == nil {
			graph.hold(each.ID, each.PkgPath, builtRestored)
			return
		}
		graph.hold(each.ID, each.PkgPath, builtFromSource)
		for _, file := range each.Syntax {
			for _, imported := range file.Imports {
				graph.imported[strings.Trim(imported.Path.Value, "\"`")] = true
			}
		}
	})
	return graph
}

// hold adds the package of identifier and path to the graph, held in mode.
func (graph *reachBuiltGraph) hold(identifier string, path string, mode builtLoadMode) {
	graph.modes[identifier] = mode
	graph.identifiers[path] = append(graph.identifiers[path], identifier)
}

// holds reports whether the build's graph holds a package of identifier.
func (graph *reachBuiltGraph) holds(identifier string) bool {
	_, isHeld := graph.modes[identifier]
	return isHeld
}

// checkedFromSource reports whether the build checked the package of identifier from source. One it restored, with
// its types or without, and one it does not hold, were not.
func (graph *reachBuiltGraph) checkedFromSource(identifier string) bool {
	return graph.modes[identifier] == builtFromSource
}

// imports reports whether a file of a package the build checked from source imports the package of path, by the spec
// as written, or by path with its vendor directory taken off, which no spec spells: net/http's files import
// "golang.org/x/net/http/httpguts", the package of path vendor/golang.org/x/net/http/httpguts.
func (graph *reachBuiltGraph) imports(path string) bool {
	if graph.imported[path] {
		return true
	}
	vendored := strings.LastIndex("/"+path, "/vendor/")
	return vendored >= 0 && graph.imported[path[vendored+len("vendor/"):]]
}
