package build_dependence

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

// Add adds other's counts to counts, as one scope's counts over two graphs that share no package.
func (counts *ScopeCounts) Add(other ScopeCounts) {
	counts.Units += other.Units
	counts.DependentUnits += other.DependentUnits
	counts.Nodes += other.Nodes
	counts.DependentNodes += other.DependentNodes
	counts.Packages += other.Packages
	counts.DependentPackages += other.DependentPackages
	counts.DependentPackageNodes += other.DependentPackageNodes
	counts.BuildOnlyUnits += other.BuildOnlyUnits
	counts.BuildOnlyNodes += other.BuildOnlyNodes
}
