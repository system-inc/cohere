package unused_code

import (
	"go/token"
	"go/types"
	"slices"
	"strings"
)

// ignoredKey is a line a //lint:ignore U1000 directive covers, or a whole file, with line -1, that a
// //lint:file-ignore U1000 covers.
type ignoredKey struct {
	file string
	line int
}

// entry builds the graph from the package's entry points: the go:linkname comments, every declaration, the generated
// files, the methods that make a named type implement an interface, and the objects a directive ignores.
func (graph *graph) entry() {
	for _, file := range graph.files {
		for _, group := range file.Comments {
			for _, comment := range group.List {
				if strings.HasPrefix(comment.Text, "//go:linkname ") {
					// The original notes that this looks at every comment, where the compiler only looks at comments
					// in the left-most column; the intention is probably to look only at top-level comments.

					// (1.8) packages use symbols linked via go:linkname
					fields := strings.Fields(comment.Text)
					if len(fields) == 3 {
						linked := graph.typesPackage.Scope().Lookup(fields[1])
						if linked == nil {
							continue
						}
						graph.use(linked, nil)
					}
				}
			}
		}
	}

	for _, file := range graph.files {
		for _, declaration := range file.Decls {
			graph.declaration(declaration, nil)
		}
	}

	// (1.9) packages use objects in generated files: DefaultOptions' GeneratedIsUsed.
	for typesObject := range graph.objects {
		path := graph.fileSet.PositionFor(typesObject.Pos(), false).Filename
		if _, isGenerated := graph.generated[path]; isGenerated {
			graph.use(typesObject, nil)
		}
	}

	// We use a normal map instead of a typeutil.Map because we deduplicate these on a best effort basis, as an
	// optimization.
	allInterfaces := make(map[*types.Interface]struct{})
	for _, interfaceType := range graph.interfaceTypes {
		allInterfaces[interfaceType] = struct{}{}
	}
	for _, instance := range graph.typesInfo.Instances {
		if named, isNamed := instance.Type.(*types.Named); isNamed && named.Obj().Pkg() == graph.typesPackage {
			if interfaceType, isInterface := named.Underlying().(*types.Interface); isInterface {
				allInterfaces[interfaceType] = struct{}{}
			}
		}
	}
	processMethodSet := func(named *types.TypeName, methodSet *types.MethodSet) {
		// DefaultOptions' ExportedIsUsed.
		for method := range methodSet.Methods() {
			if token.IsExported(method.Obj().Name()) {
				// (2.1) named types use exported methods
				// (6.4) structs use embedded fields that have exported methods
				//
				// By reading the selection, we read all embedded fields that are part of the path
				graph.readSelection(method, named)
			}
		}

		if _, isInterface := named.Type().Underlying().(*types.Interface); !isInterface {
			// (8.0) handle interfaces
			//
			// We don't care about interfaces implementing interfaces; all their methods are already used, anyway
			for interfaceType := range allInterfaces {
				selections, implementsInterface := implements(named.Type(), interfaceType, methodSet)
				if !implementsInterface {
					continue
				}
				for _, selection := range selections {
					// (8.2) any concrete type implements all known interfaces
					// (6.3) structs use embedded fields that help implement interfaces
					graph.readSelection(selection, named)
				}
			}
		}
	}

	for _, named := range graph.namedTypes {
		processMethodSet(named, types.NewMethodSet(named.Type()))
		processMethodSet(named, types.NewMethodSet(types.NewPointer(named.Type())))
	}

	ignores := map[ignoredKey]struct{}{}
	for _, directive := range graph.directives {
		if directive.Command != "ignore" && directive.Command != "file-ignore" {
			continue
		}
		if len(directive.Arguments) == 0 {
			continue
		}
		if slices.Contains(strings.Split(directive.Arguments[0], ","), "U1000") {
			position := graph.fileSet.PositionFor(directive.Node.Pos(), false)
			var key ignoredKey
			switch directive.Command {
			case "ignore":
				key = ignoredKey{position.Filename, position.Line}
			case "file-ignore":
				key = ignoredKey{position.Filename, -1}
			}

			ignores[key] = struct{}{}
		}
	}

	if len(ignores) > 0 {
		graph.useIgnored(ignores)
	}
}

// useIgnored uses every object annotated with a //lint:ignore U1000, and the methods and fields of an ignored type.
// The original does this inside entry; the port names it, entry's one deep nesting.
//
// Like the original it ranges over graph.objects while use may add to it, a method or field seen for the first time,
// so which of those the range then visits too is the map's to decide, as it is the original's.
func (graph *graph) useIgnored(ignores map[ignoredKey]struct{}) {
	for typesObject := range graph.objects {
		position := graph.fileSet.PositionFor(typesObject.Pos(), false)
		_, isIgnored := ignores[ignoredKey{position.Filename, position.Line}]
		if !isIgnored {
			_, isIgnored = ignores[ignoredKey{position.Filename, -1}]
		}
		if !isIgnored {
			continue
		}
		graph.use(typesObject, nil)

		// use methods and fields of ignored types
		typeName, isTypeName := typesObject.(*types.TypeName)
		if !isTypeName {
			continue
		}
		if typeName.IsAlias() {
			// DefaultOptions' ExportedIsUsed.
			if named, isNamed := types.Unalias(typeName.Type()).(*types.Named); isNamed &&
				(named.Obj().Pkg() != typeName.Pkg() || named.Obj().Pkg() == nil) {
				// This is an alias of a named type in another package. Don't walk its fields or methods; we don't
				// have to.
				//
				// For aliases to types in the same package, we do want to ignore the fields and methods, because
				// ignoring the alias should ignore the aliased type.
				continue
			}
		}
		if named, isNamed := types.Unalias(typeName.Type()).(*types.Named); isNamed {
			for method := range named.Methods() {
				graph.use(method, nil)
			}
		}
		if structType, isStruct := typeName.Type().Underlying().(*types.Struct); isStruct {
			for field := range structType.Fields() {
				graph.use(field, nil)
			}
		}
	}
}
