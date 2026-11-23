package main

// censusReport is the whole census, written to --json.
type censusReport struct {
	Root                string                    `json:"root"`
	Patterns            []string                  `json:"patterns"`
	Tests               bool                      `json:"tests"`
	Workspace           string                    `json:"workspace"`
	StandardLibraryRule string                    `json:"standardLibraryRule"`
	Host                configurationReport       `json:"host"`
	Builds              []buildReport             `json:"builds"`
	OverAll             map[string]*overAllCounts `json:"overAll"`
	Samples             []forkRecord              `json:"samples"`
	Notes               []string                  `json:"notes"`
}

// configurationReport is one configuration as it was loaded.
type configurationReport struct {
	Target         string   `json:"target"`
	CgoEnabled     bool     `json:"cgoEnabled"`
	CgoChoice      string   `json:"cgoChoice"`
	Packages       int      `json:"packages"`
	PackageErrors  int      `json:"packageErrors"`
	FirstErrors    []string `json:"firstErrors,omitempty"`
	LoadSeconds    float64  `json:"loadSeconds"`
	CompareSeconds float64  `json:"compareSeconds,omitempty"`
}

// buildReport is one extra build against the host, by group.
type buildReport struct {
	Configuration configurationReport     `json:"configuration"`
	Groups        map[string]*groupCounts `json:"groups"`
}

// groupCounts is one group's counts of one build against the host. A file both a package and its test variant compile
// is counted once, under the package, and so is a declaration; every count but the packages' is of distinct files,
// declarations and expressions.
type groupCounts struct {
	PairedPackages      int      `json:"pairedPackages"`
	PackagesOnlyOnHost  []string `json:"packagesOnlyOnHost"`
	PackagesOnlyInBuild []string `json:"packagesOnlyInBuild"`
	// PackagesNotCompared are paired packages one side never type-checked, whose expressions were not aligned.
	PackagesNotCompared []string `json:"packagesNotCompared,omitempty"`

	FilesShared      int       `json:"filesShared"`
	FilesOnlyInBuild fileCount `json:"filesOnlyInBuild"`
	FilesOnlyOnHost  fileCount `json:"filesOnlyOnHost"`

	Declarations              int            `json:"declarations"`
	DifferingDeclarations     int            `json:"differingDeclarations"`
	DifferingDeclarationShare float64        `json:"differingDeclarationShare"`
	DifferingByPackage        []packageCount `json:"differingByPackage"`
	DifferingDeclarationNames []string       `json:"differingDeclarationNames"`

	AlignedExpressions     int     `json:"alignedExpressions"`
	ForkedExpressions      int     `json:"forkedExpressions"`
	ForkedByReference      int     `json:"forkedByReference"`
	ForkedLocal            int     `json:"forkedLocal"`
	ForkedExpressionShare  float64 `json:"forkedExpressionShare"`
	ExpressionsOnlyOnHost  int     `json:"expressionsOnlyOnHost"`
	ExpressionsOnlyInBuild int     `json:"expressionsOnlyInBuild"`

	Functions                  int      `json:"functions"`
	ForkedFunctions            int      `json:"forkedFunctions"`
	ForkedFunctionShare        float64  `json:"forkedFunctionShare"`
	ForkedFunctionNames        []string `json:"forkedFunctionNames"`
	ForkPointsOutsideFunctions int      `json:"forkPointsOutsideFunctions"`
	PackagesWithFork           int      `json:"packagesWithFork"`
	PackagePaths               int      `json:"packagePaths"`

	// Forks is every fork of ours; the other groups' are only sampled, in the report's Samples.
	Forks []forkRecord `json:"forks,omitempty"`

	differingByPackage map[string]int
	functions          map[string]bool
	forkedFunctions    map[string]string
	packagesWithFork   map[string]bool
	packagePaths       map[string]bool
}

// fileCount is files one side compiles and the other does not, with their lines; OutsidePackageDirectory counts those
// that are not among the package's Go files, cgo's output or another generated file, whose path never pairs.
type fileCount struct {
	Files                   int `json:"files"`
	Lines                   int `json:"lines"`
	OutsidePackageDirectory int `json:"outsidePackageDirectory"`
}

// packageCount is a count for one package path.
type packageCount struct {
	Package string `json:"package"`
	Count   int    `json:"count"`
}

// forkRecord is one forked expression of one build, for a reader to check by eye.
type forkRecord struct {
	Build       string `json:"build"`
	Group       string `json:"group"`
	File        string `json:"file"`
	Line        int    `json:"line"`
	Start       int    `json:"start"`
	End         int    `json:"end"`
	Expression  string `json:"expression"`
	HostAnswer  string `json:"hostAnswer"`
	BuildAnswer string `json:"buildAnswer"`
	Class       string `json:"class"`
	Function    string `json:"function"`
}

// overAllCounts is one group over every build: distinct forked expressions and functions against the host's own, and
// how many distinct answers each forked declaration and expression has across the host and every build.
type overAllCounts struct {
	HostExpressions       int     `json:"hostExpressions"`
	ForkedExpressions     int     `json:"forkedExpressions"`
	ForkedExpressionShare float64 `json:"forkedExpressionShare"`
	HostFunctions         int     `json:"hostFunctions"`
	ForkedFunctions       int     `json:"forkedFunctions"`
	ForkedFunctionShare   float64 `json:"forkedFunctionShare"`
	DifferingDeclarations int     `json:"differingDeclarations"`
	// DeclarationVariants and ExpressionVariants map a count of distinct answers to how many forked declarations or
	// expressions have that many.
	DeclarationVariants map[int]int `json:"declarationVariants"`
	ExpressionVariants  map[int]int `json:"expressionVariants"`
	// Variants spells every forked declaration and expression of ours with its answers.
	Variants []variantRecord `json:"variants,omitempty"`
}

// variantRecord is one forked declaration or expression and its distinct answers across the host and every build.
type variantRecord struct {
	Kind    string   `json:"kind"`
	Key     string   `json:"key"`
	Answers []string `json:"answers"`
}

// The classes of a fork: one whose subtree uses a differing declaration, and one computed where it stands.
const (
	classByReference = "byReference"
	classLocal       = "local"
)
