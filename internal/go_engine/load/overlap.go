package load

import (
	"golang.org/x/tools/go/packages"
)

// Overlap is a load's graph as it stands before any package is type-checked, every package's imports known (see
// connect), for a caller that analyzes beside the load rather than after it: the packages to analyze, and when each
// package is final for analysis. Nothing of a package but its ID, files and imports may be read before Ready closes.
type Overlap struct {
	// Packages are the ones to analyze, as Loaded.Packages will hold them.
	Packages []*packages.Package

	// Rewritten is Loaded.Rewritten as the load fills it, which holds a package's rewritten files once Ready closes.
	Rewritten *Rewritten

	nodes map[*packages.Package]*node

	// ended is closed when the load returns, whatever it returns.
	ended <-chan struct{}
}

// Ready is closed once analyzed is final for analysis: its syntax, types, errors and IllTyped set, none of them
// changed by the load again. For almost every package that is when its own type check is done (loader.check), which
// sets its IllTyped from its imports', checked before it, adds the imports cgo would have given its types
// (addCgoImports), and judges its withheld errors. A dependency that withholds what reaches its cgo files is final
// only when the load ends: withholdCgoDeclarations strips its declarations' bodies and initial values once every
// package is checked. A package this load does not hold is ready at its end too, so nothing is read early by mistake.
func (overlap *Overlap) Ready(analyzed *packages.Package) <-chan struct{} {
	current, held := overlap.nodes[analyzed]
	if !held || current.withholds {
		return overlap.ended
	}
	return current.done
}

// PackagesOverlapping is Packages calling begin once the graph is built and before any package is type-checked,
// on the load's goroutine, so the caller can start analyzing beside the load. It never reads the findings cache, so
// every package is type-checked from source and closes its readiness as it is.
func PackagesOverlapping(
	directory string,
	patterns []string,
	environment []string,
	census []string,
	begin func(*Overlap),
) (*Loaded, error) {
	return loadWorkspace(directory, patterns, environment, census, true, Caching{Begin: begin}, nil)
}
