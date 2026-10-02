package program

import (
	"crypto/sha256"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/tspath"
)

// TypeFingerprints gives each project file a hash of everything its types can depend on, so a
// type-aware rule's findings for a file can be cached on the file's bytes plus this.
//
// A type-aware rule asks the checker about nodes in one file, and the checker's answers come from that
// file, every file it reaches through imports, and everything global. So the fingerprint covers:
//
//   - the file's import closure among project files, hashed per strongly connected component so import
//     cycles, which real trees have, are one unit, and so a file reached by 40% of the tree is hashed
//     once rather than once per importer;
//   - a global component folded into every fingerprint: every file in the program that is not a
//     project file (installed declarations), and every project file that can change other files' types
//     without being imported: declaration files, scripts, and anything that says `declare global` or
//     `declare module`.
//
// The global component is a superset on purpose. Including a file that turns out not to matter costs a
// miss when it changes; leaving out one that does would replay a type-aware finding computed against
// types that are gone, which is the stale verdict this cache must never produce. The lib files are not
// here because they are embedded in the binary, which is in the cache's key.
//
// Edges come from the program's own module resolution, normalised the way DependentClosure normalises
// them: compared raw, on a case-insensitive volume, they produced a graph with almost no edges, which
// reads as valid rather than as broken.
func (g *Graph) TypeFingerprints() map[tspath.Path][sha256.Size]byte {
	global, edges, contents := g.typeGraph()
	return fingerprintComponents(g.ProjectFiles(), edges, contents, global)
}

// typeGraph is what every fingerprint is built over: the global component, hashing every file that can
// change other files' types without being imported, the import edges between project files, and every
// file's content hash, computed once for all three.
func (g *Graph) typeGraph() ([sha256.Size]byte, map[tspath.Path][]tspath.Path, map[tspath.Path][sha256.Size]byte) {
	allFiles := g.Program.GetSourceFiles()
	projectFiles := g.ProjectFiles()

	contents := hashContentsInParallel(allFiles)

	isProject := make(map[tspath.Path]bool, len(projectFiles))
	for _, sourceFile := range projectFiles {
		isProject[sourceFile.Path()] = true
	}

	global := sha256.New()
	globalPaths := make([]tspath.Path, 0, len(allFiles))
	for _, sourceFile := range allFiles {
		if !isProject[sourceFile.Path()] || reachesBeyondItsImports(sourceFile) {
			globalPaths = append(globalPaths, sourceFile.Path())
		}
	}
	sort.Slice(globalPaths, func(first, second int) bool { return globalPaths[first] < globalPaths[second] })
	for _, path := range globalPaths {
		sum := contents[path]
		global.Write([]byte(path))
		global.Write([]byte{0})
		global.Write(sum[:])
	}
	var globalSum [sha256.Size]byte
	copy(globalSum[:], global.Sum(nil))

	edges := make(map[tspath.Path][]tspath.Path, len(projectFiles))
	for fromPath, resolutions := range g.Program.GetResolvedModules() {
		if !isProject[fromPath] {
			continue
		}
		for _, resolution := range resolutions {
			if resolution == nil || resolution.ResolvedFileName == "" {
				continue
			}
			toward := g.pathFor(resolution.ResolvedFileName)
			if isProject[toward] && toward != fromPath {
				edges[fromPath] = append(edges[fromPath], toward)
			}
		}
	}
	return globalSum, edges, contents
}

// reachesBeyondItsImports reports whether a project file can change other files' types without being
// imported by them. Detected generously, by kind and by text, since a false positive costs a miss.
func reachesBeyondItsImports(sourceFile *ast.SourceFile) bool {
	if sourceFile.IsDeclarationFile || sourceFile.ExternalModuleIndicator == nil {
		return true
	}
	text := sourceFile.Text()
	return strings.Contains(text, "declare global") || strings.Contains(text, "declare module")
}

// hashContentsInParallel hashes every file's text. The texts are already in memory; this is the only
// per-run cost the fingerprints add over the walk.
func hashContentsInParallel(files []*ast.SourceFile) map[tspath.Path][sha256.Size]byte {
	sums := make([][sha256.Size]byte, len(files))
	workers := runtime.NumCPU()
	var waitGroup sync.WaitGroup
	for worker := range workers {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			for index := worker; index < len(files); index += workers {
				sums[index] = sha256.Sum256([]byte(files[index].Text()))
			}
		}()
	}
	waitGroup.Wait()
	contents := make(map[tspath.Path][sha256.Size]byte, len(files))
	for index, sourceFile := range files {
		contents[sourceFile.Path()] = sums[index]
	}
	return contents
}

// fingerprintComponents runs Tarjan's algorithm over the project's import graph. Tarjan emits each
// strongly connected component after every component it reaches, so a component's hash can include its
// dependencies' finished hashes in one pass. A component's hash covers the global component, its
// members' paths and contents in order, and its dependencies' hashes in order; every member gets it.
func fingerprintComponents(projectFiles []*ast.SourceFile, edges map[tspath.Path][]tspath.Path,
	contents map[tspath.Path][sha256.Size]byte, global [sha256.Size]byte) map[tspath.Path][sha256.Size]byte {

	index := 0
	indices := make(map[tspath.Path]int, len(projectFiles))
	lowlinks := make(map[tspath.Path]int, len(projectFiles))
	onStack := make(map[tspath.Path]bool, len(projectFiles))
	stack := []tspath.Path{}
	componentOf := make(map[tspath.Path]int, len(projectFiles))
	componentSums := [][sha256.Size]byte{}
	fingerprints := make(map[tspath.Path][sha256.Size]byte, len(projectFiles))

	var connect func(path tspath.Path)
	connect = func(path tspath.Path) {
		indices[path], lowlinks[path] = index, index
		index++
		stack = append(stack, path)
		onStack[path] = true

		for _, toward := range edges[path] {
			if _, visited := indices[toward]; !visited {
				connect(toward)
				lowlinks[path] = min(lowlinks[path], lowlinks[toward])
			} else if onStack[toward] {
				lowlinks[path] = min(lowlinks[path], indices[toward])
			}
		}
		if lowlinks[path] != indices[path] {
			return
		}

		members := []tspath.Path{}
		for {
			top := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			onStack[top] = false
			members = append(members, top)
			if top == path {
				break
			}
		}
		sort.Slice(members, func(first, second int) bool { return members[first] < members[second] })

		component := len(componentSums)
		dependencies := map[int]bool{}
		for _, member := range members {
			componentOf[member] = component
		}
		for _, member := range members {
			for _, toward := range edges[member] {
				if other := componentOf[toward]; other != component {
					dependencies[other] = true
				}
			}
		}

		hash := sha256.New()
		hash.Write(global[:])
		for _, member := range members {
			sum := contents[member]
			hash.Write([]byte(member))
			hash.Write([]byte{0})
			hash.Write(sum[:])
		}
		dependencySums := make([]string, 0, len(dependencies))
		for other := range dependencies {
			dependencySums = append(dependencySums, string(componentSums[other][:]))
		}
		sort.Strings(dependencySums)
		for _, dependency := range dependencySums {
			hash.Write([]byte(dependency))
		}
		var sum [sha256.Size]byte
		copy(sum[:], hash.Sum(nil))
		componentSums = append(componentSums, sum)
		for _, member := range members {
			fingerprints[member] = sum
		}
	}

	for _, sourceFile := range projectFiles {
		if _, visited := indices[sourceFile.Path()]; !visited {
			connect(sourceFile.Path())
		}
	}
	return fingerprints
}
