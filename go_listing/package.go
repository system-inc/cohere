package go_listing

import (
	"path/filepath"
	"strings"
)

// A Package is one object of go list's -json output. A field go list wasn't asked for (a -json field
// list that leaves it out) is empty. A field whose name the house spells apart from go list's is
// tagged with go list's.
type Package struct {
	// ImportPath is the package's path, with a test variant's " [p.test]" after it when -test listed
	// one, and "p.test" for the test main go generates.
	ImportPath string
	Name       string
	// ForTest is p for each package -test compiled again for p's test: p's own variants and any
	// dependency that imports p. It is empty for the test main.
	ForTest   string
	Directory string `json:"Dir"`
	Standard  bool
	// DepOnly is whether -deps listed the package only as a dependency, matched by no pattern.
	DepOnly bool
	// Incomplete is whether go list met an error in this package or one it imports.
	Incomplete bool

	GoFiles           []string
	CgoFiles          []string
	CFiles            []string
	CxxFiles          []string `json:"CXXFiles"`
	MFiles            []string
	HFiles            []string
	FFiles            []string
	SFiles            []string
	SwigFiles         []string
	SwigCxxFiles      []string `json:"SwigCXXFiles"`
	SysoFiles         []string
	EmbedFiles        []string
	IgnoredGoFiles    []string
	IgnoredOtherFiles []string
	// InvalidGoFiles are the Go files go list met a problem in, such as a syntax error before the
	// imports end. Each is in another list too.
	InvalidGoFiles  []string
	CompiledGoFiles []string
	TestGoFiles     []string
	XTestGoFiles    []string
	TestEmbedFiles  []string
	XTestEmbedFiles []string

	CgoCFlags   []string `json:"CgoCFLAGS"`
	CgoCppFlags []string `json:"CgoCPPFLAGS"`
	CgoCxxFlags []string `json:"CgoCXXFLAGS"`

	Imports      []string
	ImportMap    map[string]string
	Deps         []string
	TestImports  []string
	XTestImports []string

	// Export is the export data's file, which -export compiles.
	Export string
	Module *Module
	// Error is the package's own error, and DepsErrors those of the packages it imports, directly or
	// not. Without -e go list exits nonzero when any package has one.
	Error      *PackageError
	DepsErrors []*PackageError
}

// Owner is the package a test variant is of: "p" of "p [p.test]", "p_test" of "p_test [p.test]",
// and the import path itself for any other package.
func (listed Package) Owner() string {
	owner, _, _ := strings.Cut(listed.ImportPath, " ")
	return owner
}

// IsTestMain is whether the package is the main package -test generates for p's test binary, "p.test".
// Its GoFiles are in go's own build cache, named by absolute paths.
func (listed Package) IsTestMain() bool {
	return listed.Name == "main" &&
		listed.ForTest == "" &&
		strings.HasSuffix(listed.ImportPath, ".test") &&
		!strings.Contains(listed.ImportPath, " ")
}

// Path is where a file go list names for the package is: name itself when it is absolute (the test
// main's sources, and -compiled's cgo output, are in go's cache), else name in the package's Directory.
func (listed Package) Path(name string) string {
	if filepath.IsAbs(name) {
		return name
	}
	return filepath.Join(listed.Directory, name)
}
