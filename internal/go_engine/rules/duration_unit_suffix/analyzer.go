// Package duration_unit_suffix is staticcheck's `ST1011`, a variable of type time.Duration, or a pointer to one,
// whose name ends in a unit of time such as "Ms" or "Secs", though a duration counts nanoseconds whatever it is named,
// ported onto the engine's syntax dispatcher.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 stylecheck/st1011/st1011.go, whose messages it
// reports word for word, at the name, with no range option and no filter for generated files, as the original does.
// The original walks the same three kinds of node with the inspector, which the dispatcher's one walk now feeds, and
// requires nothing else, so the port saves only its walk of its own.
//
// The original's MergeIf, MergeIfAny, tells staticcheck's runner to keep a finding any one build configuration makes,
// and the engine runs one configuration, so there is nothing for the port to merge.
package duration_unit_suffix

import (
	"fmt"
	"go/ast"
	"go/token"
	"strings"

	"honnef.co/go/tools/go/types/typeutil"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// Name is the original's code, which the port takes, so the rule, its settings and every `//cohere:ignore` naming it
// read the same as before the port took the original's place.
const Name = "ST1011"

// Doc is the original's title.
const Doc = "Poorly chosen name for variable of type 'time.Duration'"

// unitSuffixes are the endings the original reads as a unit of time, in its order, the first a name ends in being the
// one its finding names. Matching is case-sensitive, so "timeoutMs" ends in "Ms" and "totalms" in none.
var unitSuffixes = []string{
	"Sec", "Secs", "Seconds",
	"Msec", "Msecs",
	"Milli", "Millis", "Milliseconds",
	"Usec", "Usecs", "Microseconds",
	"MS", "Ms",
}

// Listener is what the port hears of each package from the dispatcher: every declaration of a variable, every list
// of fields or parameters, and every assignment, the three places the original reads a name being declared.
var Listener = rule.Listener{
	FileLocal: true,
	Nodes:     []ast.Node{(*ast.ValueSpec)(nil), (*ast.FieldList)(nil), (*ast.AssignStmt)(nil)},
	Node:      checkNode,
}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener.
var Analyzer = dispatch.RuleAnalyzer(rule.Namespace+Name, Name, Doc, &Listener)

// checkNode judges the names node declares: a var or const specification's names, each field's or parameter's names,
// and the plain identifiers on the left of a short variable declaration.
func checkNode(context *rule.Context, node ast.Node, _ []ast.Node) {
	switch node := node.(type) {
	case *ast.ValueSpec:
		checkNames(context, node.Names)
	case *ast.FieldList:
		for _, field := range node.List {
			checkNames(context, field.Names)
		}
	case *ast.AssignStmt:
		// A plain assignment declares nothing, so only a short variable declaration is read.
		if node.Tok != token.DEFINE {
			return
		}
		names := []*ast.Ident{}
		for _, left := range node.Lhs {
			if name, isName := left.(*ast.Ident); isName {
				names = append(names, name)
			}
		}
		checkNames(context, names)
	}
}

// checkNames reports each name that declares something of type time.Duration, or a pointer to it, and ends in a unit
// of time.
func checkNames(context *rule.Context, names []*ast.Ident) {
	for _, name := range names {
		// A name the type checker recorded no definition for is a use, as a variable a short declaration redeclares
		// is, and is judged where it was declared. The original asks only whether the name is recorded, so a
		// definition with no object, which has no type, is left by the type check below rather than here.
		if _, isDefinition := context.TypesInfo.Defs[name]; !isDefinition {
			continue
		}
		nameType := context.TypesInfo.TypeOf(name)
		if !typeutil.IsTypeWithName(nameType, "time.Duration") &&
			!typeutil.IsPointerToTypeWithName(nameType, "time.Duration") {
			continue
		}
		for _, suffix := range unitSuffixes {
			if strings.HasSuffix(name.Name, suffix) {
				staticcheck_report.Report(context, name,
					fmt.Sprintf("var %s is of type %v; don't use unit-specific suffix %q", name.Name, nameType, suffix))
				break
			}
		}
	}
}
