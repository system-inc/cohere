// Package struct_tag is vet's structtag pass, a struct field tag that reflect.StructTag.Get cannot read as written,
// a json or xml tag on a field those encodings never see because it is unexported, and a json or xml name two
// fields of one struct both claim, embedded structs' fields included at the depth their embedding puts them.
//
// It is translated, not copied, from golang.org/x/tools@v0.50.0 go/analysis/passes/structtag/structtag.go, whose
// logic it keeps and whose messages it reports word for word, at the same position. The registry runs the port in the
// original's place, and the original stays the oracle: the parity tests beside this file run both over the same
// packages. The two agree everywhere, so there is no differences fixture. The code differs in one place that changes
// no finding: whether the package is under encoding/, which the original asks of every field, is asked once.
package struct_tag

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"path/filepath"
	"reflect"
	"slices"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

// Analyzer runs the port under the original's name, documentation and address, so the rule, its settings and every
// `//cohere:ignore` naming it read the same as before the port took the original's place. It runs despite type
// errors, as the original does, since a tag is judged from the struct's fields alone.
var Analyzer = &analysis.Analyzer{
	Name: "structtag",
	Doc: "check that struct field tags conform to reflect.StructTag.Get\n\n" +
		"Also report certain struct tags (json, xml) used with unexported fields.",
	URL:              "https://pkg.go.dev/golang.org/x/tools/go/analysis/passes/structtag",
	Requires:         []*analysis.Analyzer{inspect.Analyzer},
	RunDespiteErrors: true,
	Run:              run,
}

// encodedName is one name an encoding gives a field, and the embedding depth the field sits at below the struct
// being judged, the top level being 1. Equal names conflict only at the same depth, since a shallower field shadows
// a deeper one. x/tools' uniqueName.
type encodedName struct {
	// key is the encoding, `json`, `xml`, or `xml attribute`, since xml's attributes have names of their own.
	key string
	// name is the name the tag gives, its options cut off.
	name string
	// level is the depth of embedding, 1 for a field of the struct itself.
	level int
}

// duplicateCheckedKeys are the encodings whose names must not repeat across a struct's fields. x/tools'
// checkTagDups.
var duplicateCheckedKeys = []string{"json", "xml"}

func run(pass *analysis.Pass) (any, error) {
	inspected, isInspector := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	if !isInspector {
		// Invariant: the driver hands every analyzer the results of what it requires, of the types they declare.
		panic("struct_tag: inspect's result is missing")
	}
	// The encoding packages know how to use their own tags, and sometimes test what happens to wrong ones. The
	// original asks this of every field it checks, and the answer is the package's alone, so it is asked once here.
	if strings.HasPrefix(pass.Pkg.Path(), "encoding/") {
		return nil, nil
	}
	// All visits each struct type in preorder, as the original's Preorder does, and hands it over typed.
	for structType := range inspector.All[*ast.StructType](inspected) {
		// Type information may be incomplete, since the pass runs despite errors.
		structure, isStruct := pass.TypesInfo.Types[structType].Type.(*types.Struct)
		if !isStruct {
			continue
		}
		// Each struct type remembers the names its own fields claim, and no other's.
		seen := claimedNames{}
		for index := range structure.NumFields() {
			checkCanonicalFieldTag(pass, structure.Field(index), structure.Tag(index), seen)
		}
	}
	return nil, nil
}

// checkCanonicalFieldTag judges one field of a struct and its tag: the names it claims against those seen, the tag's
// syntax, and a json or xml tag on an unexported field.
func checkCanonicalFieldTag(pass *analysis.Pass, field *types.Var, tag string, seen claimedNames) {
	for _, key := range duplicateCheckedKeys {
		checkTagDuplicates(pass, tag, key, field, field, seen, 1)
	}
	if validateError := validateStructTag(tag); validateError != nil {
		pass.Report(analysis.Diagnostic{
			Pos: field.Pos(),
			End: field.Pos() + token.Pos(len(field.Name())),
			Message: fmt.Sprintf(
				"struct field tag %#q not compatible with reflect.StructTag.Get: %s", tag, validateError,
			),
		})
	}
	// An embedded field is left alone, as the original leaves it pending what Go issue 7363 settled.
	if field.Anonymous() || field.Exported() {
		return
	}
	for _, encoding := range [...]string{"json", "xml"} {
		// No tag for the encoding, or a name of `-`, which tells it to skip the field, says nothing the field being
		// unexported does not already say.
		switch reflect.StructTag(tag).Get(encoding) {
		case "", "-":
			continue
		}
		pass.Report(analysis.Diagnostic{
			Pos:     field.Pos(),
			End:     field.Pos() + token.Pos(len(field.Name())),
			Message: fmt.Sprintf("struct field %s has %s tag but is not exported", field.Name(), encoding),
		})
		return
	}
}

// checkTagDuplicates records the name field's tag gives under key, at level, or reports it when a field already
// claimed it there. A field the tag gives no name, when it is embedded, claims the names of its struct's exported
// fields one level down. nearest is the field of the struct being judged that field is, or is embedded below, and
// is where a repeat is reported.
func checkTagDuplicates(
	pass *analysis.Pass,
	tag string,
	key string,
	nearest *types.Var,
	field *types.Var,
	seen claimedNames,
	level int,
) {
	value := reflect.StructTag(tag).Get(key)
	// The encoding skips the field, embedded or not.
	if value == "-" {
		return
	}
	if value == "" || value[0] == ',' {
		// A field that is not embedded and has no name of its own goes by its field name, which is not checked.
		if !field.Anonymous() {
			return
		}
		// An embedded pointer, or a type that is not a struct, is not looked into.
		embedded, isStruct := field.Type().Underlying().(*types.Struct)
		if !isStruct {
			return
		}
		for index := range embedded.NumFields() {
			embeddedField := embedded.Field(index)
			if !embeddedField.Exported() {
				continue
			}
			checkTagDuplicates(pass, embedded.Tag(index), key, nearest, embeddedField, seen, level+1)
		}
		return
	}
	// XMLName names the struct's own element, which cannot collide with the names its other fields give. A struct
	// field without a tag whose own XMLName names it is not checked either; see Go issue 18256.
	if key == "xml" && field.Name() == "XMLName" {
		return
	}
	// name is the name the tag gives, and namespace the key it is recorded under.
	name := value
	namespace := key
	if comma := strings.Index(value, ","); comma >= 0 {
		// xml's attributes have names of their own, apart from its elements'. The key is part of the message.
		if key == "xml" && slices.Contains(strings.Split(value[comma:], ","), "attr") {
			namespace = key + " attribute"
		}
		name = value[:comma]
	}
	claimedAt, isClaimed := seen.claim(encodedName{key: namespace, name: name, level: level}, field.Pos())
	if !isClaimed {
		return
	}
	// The other position is named relative to this field's directory, without its column, so the message is
	// unambiguous even when the two fields sit in different packages and directories through embedding.
	also := pass.Fset.Position(claimedAt)
	also.Column = 0
	relative, relativeError := filepath.Rel(filepath.Dir(pass.Fset.Position(field.Pos()).Filename), also.Filename)
	// Rel fails only when one path is absolute and the other is not, which a loaded package's files never are, and
	// then the file name is left as it is, as the original leaves it.
	if relativeError == nil {
		also.Filename = relative
	}
	pass.Report(analysis.Diagnostic{
		Pos:     nearest.Pos(),
		End:     nearest.Pos() + token.Pos(len(nearest.Name())),
		Message: fmt.Sprintf("struct field %s repeats %s tag %q also at %s", field.Name(), namespace, name, also),
	})
}
