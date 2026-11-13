package build_dependence

import (
	"go/types"
	"sort"
)

// Dependence is one build's judgment of the graph's units and objects (Against).
type Dependence struct {
	resolved      *resolvedGraph
	unitDependent []bool
	bits          []bool
	counts        Counts
}

// Bits are an object's two bits in one build. A named type has both; any other object only Name.
type Bits struct {
	Name       bool
	Underlying bool
}

// Counts are one build's counts, by scope (see Against's count).
type Counts struct {
	Workspace    ScopeCounts
	Dependencies ScopeCounts
}

// ScopeCounts are one scope's counts in one build. Nodes are syntax nodes, a unit's size. Packages are the packages
// with units, and DependentPackages those with a dependent unit or a unit only the build compiles, whose nodes
// DependentPackageNodes are: what a package-wide rule run whole on each of them would have to redo. BuildOnlyUnits and
// BuildOnlyNodes are the units only the build compiles, in the scope's packages and in packages only the build has,
// which no total of the host's holds.
type ScopeCounts struct {
	Units                 int
	DependentUnits        int
	Nodes                 int
	DependentNodes        int
	Packages              int
	DependentPackages     int
	DependentPackageNodes int
	BuildOnlyUnits        int
	BuildOnlyNodes        int
}

// UnitDependent reports whether the unit of the package packageId that starts at offset is build-dependent: file is the
// one a reader sees, as the unit's start's position names it past any //line directive, and offset is in the file the
// host parsed. A unit the graph does not hold, one in a file only the build compiles, is
// dependent by definition, and any other place is no unit the graph knows, which is never read as independent.
func (dependence *Dependence) UnitDependent(packageId string, file string, offset int) bool {
	index, isUnit := dependence.resolved.unitIndex[unitKey{packageIdentifier: packageId, file: file, offset: offset}]
	if !isUnit {
		return true
	}
	return dependence.unitDependent[index]
}

// Counts are the build's counts.
func (dependence *Dependence) Counts() Counts {
	return dependence.counts
}

// DependentObjects are the objects the build makes dependent, each as its generic declares it, with its bits, so one
// graph's answer can feed another's Difference.DependentObject.
func (dependence *Dependence) DependentObjects() map[types.Object]Bits {
	dependent := map[types.Object]Bits{}
	for node, object := range dependence.resolved.objects {
		bits := Bits{
			Name:       dependence.bits[bit(int32(node), false)],
			Underlying: dependence.bits[bit(int32(node), true)],
		}
		if bits.Name || bits.Underlying {
			dependent[object] = bits
		}
	}
	return dependent
}

// Unit is one unit of the graph as a build judged it: its key, the package ID, the file a reader sees and its start
// offset in the file the host parsed, as UnitDependent takes them; its size in syntax nodes; whether its package is
// the workspace's; whether the build makes it dependent; and the units it calls statically, by their index in Units.
type Unit struct {
	PackageId string
	File      string
	Start     int
	Nodes     int
	Workspace bool
	Dependent bool
	Calls     []int
}

// Units are every unit of the graph, as the build judged it, in the graph's order, which their indexes name.
func (dependence *Dependence) Units() []Unit {
	units := make([]Unit, 0, len(dependence.resolved.units))
	for index := range dependence.resolved.units {
		resolved := &dependence.resolved.units[index]
		units = append(units, Unit{
			PackageId: dependence.resolved.packages[resolved.packageIndex].identifier,
			File:      resolved.record.file,
			Start:     resolved.record.start,
			Nodes:     resolved.record.nodes,
			Workspace: dependence.resolved.packages[resolved.packageIndex].workspace,
			Dependent: dependence.unitDependent[index],
			Calls:     resolved.calls,
		})
	}
	return units
}

// UnitContaining is the index in Units of the unit of the package packageId whose span in file, the one a reader
// sees, holds offset, and false when none does. An object's position names its declaring unit this way in every build
// that compiles the file, since the file's bytes, and so every offset in it, are the same in each.
func (dependence *Dependence) UnitContaining(packageId string, file string, offset int) (int, bool) {
	inFile := dependence.resolved.fileUnits[fileKey{packageIdentifier: packageId, file: file}]
	// The last unit starting at or before offset is the only one that can hold it, since a file's units never overlap.
	position := sort.Search(len(inFile), func(index int) bool {
		return dependence.resolved.units[inFile[index]].record.start > offset
	}) - 1
	if position < 0 {
		return 0, false
	}
	index := inFile[position]
	if offset >= dependence.resolved.units[index].record.end {
		return 0, false
	}
	return index, true
}
