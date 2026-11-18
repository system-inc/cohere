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

	// IdentityMethod names a method of a named type by the type's package's path, the type's name as Path, the
	// method's name as Method, and the file that declares the method. Never by its objectpath, whose `M<index>` is the
	// method's place among the type's methods in the order their files declare them, which a method another build adds
	// to an earlier file shifts.
	IdentityMethod IdentityKind = "Method"

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
// in place of the object. Method is a method's name, and Digest the declaring file's digest, when the renderer was
// given a way to read it. Two identities are compared by Equal alone, never by `==`, which the field of no size keeps
// from compiling, nor by String, since an unknown identity names no object and so matches none, another unknown's
// included.
type Identity struct {
	_           [0]func()
	Kind        IdentityKind
	PackagePath string
	Path        objectpath.Path
	Method      string
	File        string
	Ordinal     int
	Digest      string
}

// String is the identity written out: `universe.len`, `import fmt (file.go)`, `example.com/p.T.UF0 (file.go)`,
// `(example.com/p.T).Method (file.go)`.
func (identity Identity) String() string {
	switch identity.Kind {
	case IdentityUniverse:
		return "universe." + string(identity.Path)
	case IdentityImport:
		return "import " + identity.PackagePath + " (" + identity.File + ")"
	case IdentityFileOrdinal:
		return identity.File + "#" + strconv.Itoa(identity.Ordinal) + " " + string(identity.Path)
	case IdentityMethod:
		return "(" + identity.PackagePath + "." + string(identity.Path) + ")." + identity.Method +
			" (" + identity.File + ")"
	case IdentityUnknown:
		return "unknown"
	}
	return identity.PackagePath + "." + string(identity.Path) + " (" + identity.File + ")"
}

// Equal reports whether identity and other name the same object: never when either is unknown.
func (identity Identity) Equal(other Identity) bool {
	if identity.Kind == IdentityUnknown || other.Kind == IdentityUnknown {
		return false
	}
	return identity.Kind == other.Kind && identity.PackagePath == other.PackagePath && identity.Path == other.Path &&
		identity.Method == other.Method && identity.File == other.File && identity.Ordinal == other.Ordinal &&
		identity.Digest == other.Digest
}
