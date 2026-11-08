// Package struct_tag is vet's structtag pass, a struct field tag that reflect.StructTag.Get cannot read as written,
// a json or xml tag on a field those encodings never see because it is unexported, and a json or xml name two
// fields of one struct both claim, embedded structs' fields included at the depth their embedding puts them. It sits
// on the engine's syntax dispatcher, hearing each struct type on the walk the dispatcher makes once for every syntax
// rule.
//
// It is translated, not copied, from golang.org/x/tools@v0.50.0 go/analysis/passes/structtag/structtag.go, whose
// logic it keeps and whose messages it reports word for word, at the same position. The registry runs the port in the
// original's place, and the original stays the oracle: the parity tests beside this file run both over the same
// packages. The two agree everywhere, so there is no differences fixture. The code differs in one place that changes
// no finding: whether the package is under encoding/, which the original asks of every field, is asked once, on the
// package's first struct type.
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

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
)

// passName is the original's name, which the port takes, so the rule, its settings and every `//cohere:ignore`
// naming it read the same as before the port took the original's place.
const passName = "structtag"

// passDoc is the original's documentation, word for word, so the rule is described in `go vet`'s own words.
const passDoc = "check that struct field tags conform to reflect.StructTag.Get\n\n" +
	"Also report certain struct tags (json, xml) used with unexported fields."

// passUrl is the original's address.
const passUrl = "https://pkg.go.dev/golang.org/x/tools/go/analysis/passes/structtag"

// Listener is what the port hears of each package from the dispatcher: every struct type, in preorder, as the
// original's Preorder visits them, judged only in a package not under encoding/, which it asks once for the package,
// on the first struct type, and keeps in the context's state.
var Listener = rule.Listener{
	Nodes: []ast.Node{(*ast.StructType)(nil)},
	Node:  checkStruct,
}

// Analyzer runs the port under the original's name, documentation and address, reporting what the shared syntax
// dispatcher's walk found for Listener.
var Analyzer = newAnalyzer(dispatch.SharedSyntax())

// newAnalyzer registers Listener on dispatcher and gives the analyzer it returns the original's address, which
// RuleAnalyzer leaves unset. It runs despite type errors, as the original does, since a tag is judged from the
// struct's fields alone.
func newAnalyzer(dispatcher *dispatch.Syntax) *analysis.Analyzer {
	analyzer := dispatcher.RuleAnalyzer(rule.Namespace+passName, passName, passDoc, &Listener)
	analyzer.URL = passUrl
	analyzer.RunDespiteErrors = true
	return analyzer
}

// packageGate is whether the package is under encoding/, asked once, on its first struct type.
type packageGate struct {
	// decided is whether the package has been asked about yet.
	decided bool
	// isEncoding is whether the package's path is under encoding/, where nothing is judged.
	isEncoding bool
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

// checkStruct judges each field of a struct type and its tag, in a package not under encoding/.
func checkStruct(context *rule.Context, node ast.Node, _ []ast.Node) {
	structType, isStructType := node.(*ast.StructType)
	if !isStructType {
		return
	}
	// The encoding packages know how to use their own tags, and sometimes test what happens to wrong ones. The
	// original asks this of every field it checks, and the answer is the package's alone, so it is asked once here.
	gate := rule.State[packageGate](context)
	if !gate.decided {
		gate.decided, gate.isEncoding = true, strings.HasPrefix(context.Package.Path(), "encoding/")
	}
	if gate.isEncoding {
		return
	}
	// Type information may be incomplete, since the pass runs despite errors.
	structure, isStruct := context.TypesInfo.Types[structType].Type.(*types.Struct)
	if !isStruct {
		return
	}
	// Each struct type remembers the names its own fields claim, and no other's.
	seen := claimedNames{}
	for index := range structure.NumFields() {
		checkCanonicalFieldTag(context, structure.Field(index), structure.Tag(index), seen)
	}
}

// checkCanonicalFieldTag judges one field of a struct and its tag: the names it claims against those seen, the tag's
// syntax, and a json or xml tag on an unexported field.
func checkCanonicalFieldTag(context *rule.Context, field *types.Var, tag string, seen claimedNames) {
	for _, key := range duplicateCheckedKeys {
		checkTagDuplicates(context, tag, key, field, field, seen, 1)
	}
	if validateError := validateStructTag(tag); validateError != nil {
		context.Report(analysis.Diagnostic{
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
		context.Report(analysis.Diagnostic{
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
	context *rule.Context,
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
			checkTagDuplicates(context, embedded.Tag(index), key, nearest, embeddedField, seen, level+1)
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
	also := context.Fset.Position(claimedAt)
	also.Column = 0
	relative, relativeError := filepath.Rel(filepath.Dir(context.Fset.Position(field.Pos()).Filename), also.Filename)
	// Rel fails only when one path is absolute and the other is not, which a loaded package's files never are, and
	// then the file name is left as it is, as the original leaves it.
	if relativeError == nil {
		also.Filename = relative
	}
	context.Report(analysis.Diagnostic{
		Pos:     nearest.Pos(),
		End:     nearest.Pos() + token.Pos(len(nearest.Name())),
		Message: fmt.Sprintf("struct field %s repeats %s tag %q also at %s", field.Name(), namespace, name, also),
	})
}
