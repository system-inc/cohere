package build_reads

import (
	"go/token"
	"go/types"

	"golang.org/x/tools/go/types/objectpath"
)

// IdentityRenderer renders the identities of one run's objects, on one goroutine: by fileSet, the run's, which places
// each object in its file, and digest, which reads a file's digest by its name, nil when none is wanted. Its encoder
// keeps an index of each package's objectpaths as it first renders a member that needs one, reused for every later
// one. Rendered and Unknown count the identities rendered, and those of them unknown.
type IdentityRenderer struct {
	fileSet  *token.FileSet
	digest   func(file string) string
	encoder  objectpath.Encoder
	Rendered int
	Unknown  int
}

// NewIdentityRenderer is a renderer by fileSet and digest (see IdentityRenderer).
func NewIdentityRenderer(fileSet *token.FileSet, digest func(file string) string) *IdentityRenderer {
	return &IdentityRenderer{fileSet: fileSet, digest: digest}
}

// Object is object's identity. A package-level object is named by its name, an unexported one too, since the
// package's own are what a differing file of it changes, and any other by its objectpath; one with neither is
// unknown.
func (renderer *IdentityRenderer) Object(object types.Object) Identity {
	renderer.Rendered++
	identity := renderer.object(object)
	if identity.Kind == IdentityUnknown {
		renderer.Unknown++
	}
	return identity
}

// object is Object, uncounted.
func (renderer *IdentityRenderer) object(object types.Object) Identity {
	if object.Pkg() == nil {
		if types.Universe.Lookup(object.Name()) == object {
			return Identity{Kind: IdentityUniverse, Path: objectpath.Path(object.Name())}
		}
		// The universe declares one method, error's Error.
		if function, isFunction := object.(*types.Func); isFunction && function.Signature().Recv() != nil {
			return Identity{Kind: IdentityUniverse,
				Path: objectpath.Path(types.TypeString(function.Signature().Recv().Type(), nil) + "." + object.Name())}
		}
		return Identity{Kind: IdentityUnknown}
	}
	if imported, isPackageName := object.(*types.PkgName); isPackageName {
		return renderer.placed(Identity{Kind: IdentityImport, PackagePath: imported.Imported().Path()}, object.Pos())
	}
	identity := Identity{Kind: IdentityObject, PackagePath: object.Pkg().Path()}
	if object.Pkg().Scope().Lookup(object.Name()) == object {
		identity.Path = objectpath.Path(object.Name())
		return renderer.placed(identity, object.Pos())
	}
	path, pathError := renderer.encoder.For(object)
	if pathError != nil {
		return Identity{Kind: IdentityUnknown}
	}
	identity.Path = path
	return renderer.placed(identity, object.Pos())
}

// Used is entry's identity: its object's, and a selection's receiver's, each counted as Object counts it.
func (renderer *IdentityRenderer) Used(entry UsedEntry) UsedIdentity {
	identity := UsedIdentity{Object: renderer.Object(entry.Object), Index: entry.Index}
	if entry.Receiver != nil {
		identity.Receiver = renderer.Object(entry.Receiver)
	}
	return identity
}

// Declaration is the identity of each object declaration declares, or its file and ordinal when it declares none.
func (renderer *IdentityRenderer) Declaration(declaration Declaration) []Identity {
	if len(declaration.Objects) == 0 {
		renderer.Rendered++
		return []Identity{renderer.placed(Identity{Kind: IdentityFileOrdinal, Path: objectpath.Path(declaration.Name),
			Ordinal: declaration.Ordinal}, declaration.File)}
	}
	identities := make([]Identity, 0, len(declaration.Objects))
	for _, object := range declaration.Objects {
		identities = append(identities, renderer.Object(object))
	}
	return identities
}

// placed is identity with the name of the file at position, and its digest when the renderer reads digests.
func (renderer *IdentityRenderer) placed(identity Identity, position token.Pos) Identity {
	if file := renderer.fileSet.File(position); file != nil {
		identity.File = file.Name()
	}
	if renderer.digest != nil && identity.File != "" {
		identity.Digest = renderer.digest(identity.File)
	}
	return identity
}
