package global_rename

import (
	"go/token"
	"go/types"
)

// keyer computes and remembers keys.
type keyer struct {
	fileSet *token.FileSet
	keys    map[types.Object]Key
}

func (keyer *keyer) key(object types.Object) Key {
	if key, found := keyer.keys[object]; found {
		return key
	}
	declared := origin(object)
	key := Key{Name: kindOf(declared) + " " + declared.Name()}
	if declared.Pkg() != nil {
		key.PackagePath = declared.Pkg().Path()
	}
	if declared.Pos().IsValid() {
		position := keyer.fileSet.PositionFor(declared.Pos(), false)
		key = Key{
			PackagePath: key.PackagePath,
			Filename:    position.Filename,
			Line:        position.Line,
			Column:      position.Column,
		}
	}
	keyer.keys[object] = key
	return key
}
