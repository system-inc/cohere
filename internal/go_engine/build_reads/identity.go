package build_reads

import (
	"strconv"

	"golang.org/x/tools/go/types/objectpath"
)

// IdentityKind is how an Identity names its object.
type IdentityKind string

const (
	// IdentityObject names an object by its package's path, its objectpath and the file that declares it.
	IdentityObject IdentityKind = "Object"

	// IdentityUniverse names a universe object by its name, Path, and a method of the universe's error by
	// `error.Error`.
	IdentityUniverse IdentityKind = "Universe"

	// IdentityImport names an imported package's name by the package's path and the file that imports it.
	IdentityImport IdentityKind = "Import"

	// IdentityFileOrdinal names a declaration that declares nothing an objectpath reaches, an init function or `_`,
	// by its file, its name, Path, and its Ordinal among such in the file.
	IdentityFileOrdinal IdentityKind = "FileOrdinal"

	// IdentityUnknown is an object with no stable identity, a field of a type local to another function, say, which
	// another build must take as differing.
	IdentityUnknown IdentityKind = "Unknown"
)

// Identity is an object's identity, the same in every build where the object is declared alike: what crosses builds
// in place of the object. Digest is the declaring file's digest, when the renderer was given a way to read it.
type Identity struct {
	Kind        IdentityKind
	PackagePath string
	Path        objectpath.Path
	File        string
	Ordinal     int
	Digest      string
}

// String is the identity written out: `universe.len`, `import fmt (file.go)`, `example.com/p.T.M0 (file.go)`.
func (identity Identity) String() string {
	switch identity.Kind {
	case IdentityUniverse:
		return "universe." + string(identity.Path)
	case IdentityImport:
		return "import " + identity.PackagePath + " (" + identity.File + ")"
	case IdentityFileOrdinal:
		return identity.File + "#" + strconv.Itoa(identity.Ordinal) + " " + string(identity.Path)
	case IdentityUnknown:
		return "unknown"
	}
	return identity.PackagePath + "." + string(identity.Path) + " (" + identity.File + ")"
}

// UsedIdentity is a used entry's identity (UsedEntry): its object's, and for a selection, its Index and its receiver's,
// the zero Identity when it starts from no named type. Any other use has neither.
type UsedIdentity struct {
	Object   Identity
	Receiver Identity
	Index    string
}
