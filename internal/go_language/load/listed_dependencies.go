package load

// listedDependencies is which listed packages each listed package depends on, as its Deps say, read once for the
// whole graph. Every root's test variants ask what its tests reach and what depends on it, and answering from the Deps
// themselves gathered a few thousand import paths and read the whole listing for each root, which at a few hundred
// roots was most of what a graph cost to build. Here each answer is a few words of bits.
//
// The sets together are the listing's packages squared, a bit each: 160 KB at cohere's root, 1,125 packages, about
// 12 MB at ten thousand packages and about 300 MB at fifty thousand, held while newGraph runs and no longer. A listing
// of some tens of thousands of packages wants another shape: sets for the packages the roots' tests reach alone, or
// sparse ones.
type listedDependencies struct {
	// place is each listed package's place in the listing, by import path. A listing holds one package a path
	// (listing.add, the in-process lister), so a path's place is its package's, as the listing's byPath is.
	place map[string]int

	// words is how many words a set of the listing's packages takes.
	words int

	// dependsOn is, for the package at each place, the listed packages among its Deps.
	dependsOn []packageSet
}

// newListedDependencies reads every listed package's Deps once. A dependency the listing does not hold is in no set,
// and nothing asks for one: only a listed package is built again for a root's tests.
func newListedDependencies(listed *listing) *listedDependencies {
	dependencies := &listedDependencies{
		place:     make(map[string]int, len(listed.packages)),
		words:     (len(listed.packages) + 63) / 64,
		dependsOn: make([]packageSet, len(listed.packages)),
	}
	for place, listedPackage := range listed.packages {
		dependencies.place[listedPackage.ImportPath] = place
	}
	// One allocation holds every package's set, each a slice of it.
	every := make(packageSet, dependencies.words*len(listed.packages))
	for place, listedPackage := range listed.packages {
		dependencies.dependsOn[place] = every[place*dependencies.words : (place+1)*dependencies.words]
		for _, dependency := range listedPackage.Deps {
			if dependencyPlace, found := dependencies.place[dependency]; found {
				dependencies.dependsOn[place].add(dependencyPlace)
			}
		}
	}
	return dependencies
}

// depends reports whether importer depends on dependency, each named by its import path. A package the listing does
// not hold depends on nothing here, and nothing here depends on it.
func (dependencies *listedDependencies) depends(importer string, dependency string) bool {
	importerPlace, importerListed := dependencies.place[importer]
	dependencyPlace, dependencyListed := dependencies.place[dependency]
	return importerListed && dependencyListed && dependencies.dependsOn[importerPlace].has(dependencyPlace)
}
