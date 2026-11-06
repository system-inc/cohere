// Package security_no_interpolated_sql_string is the Go engine's rule that a value is bound to a SQL query as a
// parameter, not written into its text.
//
// It is the twin of the TypeScript rule nexus/security-no-interpolated-sql-string and reads SQL the same way,
// through package sqltext. A query built with `+` or fmt.Sprintf is text with holes in it, one per value written
// in, and a hole is a finding where the value's own text becomes SQL: between the single quotes of a string that
// opens where a value goes, when its type can hold a quote, or bare right after a comparison, when its type can
// hold any text. A quote in the value then ends the string early, so the query breaks on ordinary input like
// O'Brien, and a crafted value runs as SQL.
//
// # Which calls
//
// The query argument of Query, QueryRow, Exec and Prepare, and their Context forms, on database/sql's *DB, *Tx
// and *Conn, matched by the method's package and receiver type, so a method of the same name on anything else is
// never mistaken for one, while a *DB embedded in a struct of ours is still found. *Stmt's methods take only
// arguments, which are already bound, so nothing on a *Stmt is read.
//
// # How a query is read
//
// A string constant is text. `left + right` on strings is the two read in turn. A call to fmt.Sprintf whose format
// is a constant is its format's texts with a hole for each verb, as long as the verbs take their arguments one
// after another (no `[n]` index, no `*` width) and there are as many arguments as verbs; a plain `%s` or `%v` of
// a string argument is that argument read in place, since it prints the string unchanged. A local variable
// assigned once, where it is declared, whose address is never taken, is its initializer read in place, when that
// initializer is itself one of these shapes. Anything else is a hole.
//
// # What it leaves alone
//
// A hole outside any string that is not right after a comparison builds SQL from code (a table or column name,
// an `IN (?, ?, ?)` list, a condition, `LIMIT %d`, `CREATE DATABASE %s`), where no placeholder can go, so it is
// never a finding, and neither is one right after a comparison that the next text continues with `.`, since
// `= %s.id` qualifies a column with a table's name. A hole inside double quotes or backquotes is a name. A value
// that cannot hold a quote is never a finding: a number, a boolean and the text strconv formats one as (package
// inertvalue's judgment, which the shell rule shares), a constant whose text holds no `'` or `\`, a value
// formatted with `%x`, and a struct, pointer, slice or map, whose printed form is the fmt package's and calling it
// quote-capable would be a guess. A string, an interface (any or error) and a type parameter whose type set is not
// only numbers and booleans can hold one. The reading gives up on the whole query, rather than guess, wherever
// package sqltext does: a backslash inside quoted text, `#` outside a string, `--` not followed by whitespace, a
// dollar quote, and a query ending inside a string, a name or a block comment.
//
// One escape is accepted as making a value safe, the one the TypeScript rule accepts, written where the reader
// can see it: strings.ReplaceAll (or strings.Replace with -1) doubling every backslash and every single quote, as
// the outermost two calls, in either order, on the value itself or in the initializer of the local it names. With
// both doubled no input can close the string on SQLite, standard SQL, or MySQL.
//
// # What it does not see
//
// A query assembled in a strings.Builder, with fmt.Sprint or strings.Join, or by appending to a variable more
// than once, a query held in a field or a package variable, a call through an interface or a method value held
// in a variable, and a `%c` or `%q` of an integer, which prints a character that may be a quote.
package security_no_interpolated_sql_string

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"
	"unicode/utf8"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/types/typeutil"

	"github.com/system-inc/cohere/internal/goengine/rule"
	"github.com/system-inc/cohere/internal/sqltext"
	"github.com/system-inc/cohere/policy"
)

// Name is what a finding of this rule prints.
const Name = "cohere-go/security-no-interpolated-sql-string"

// Analyzer runs the rule over one package.
var Analyzer = &analysis.Analyzer{
	Name: "security_no_interpolated_sql_string",
	Doc: "Flags a value built with + or fmt.Sprintf into the query of a database/sql Query, QueryRow, Exec or " +
		"Prepare call where its text becomes SQL: inside a single-quoted string when it can hold a quote, or bare " +
		"after a comparison when it can hold text. A name, a placeholder list or a number built into a query is " +
		"left alone, since no bound parameter can take its place.",
	Run: run,
}

// Rule is this rule's entry in the Go engine's registry.
var Rule = rule.Rule{Name: Name, Origin: rule.OriginHouse, TypeAware: true, Analyzer: Analyzer}

var interpolatedSqlStringMessage = policy.MessageOf(Name, "interpolatedSqlString")

var interpolatedSqlValueMessage = policy.MessageOf(Name, "interpolatedSqlValue")

// queryArgumentIndexes is each query method's name and the index of its query parameter: after the context in a
// Context form, first otherwise. *Conn has only the Context forms, and the lookup is by the method's own object,
// so a name a receiver lacks never matches.
var queryArgumentIndexes = map[string]int{
	"Query":           0,
	"QueryRow":        0,
	"Exec":            0,
	"Prepare":         0,
	"QueryContext":    1,
	"QueryRowContext": 1,
	"ExecContext":     1,
	"PrepareContext":  1,
}

// queryReceivers are the database/sql types whose methods take a query as text.
var queryReceivers = map[string]bool{"DB": true, "Tx": true, "Conn": true}

// readingDepth bounds how far a query is read through locals, sums and Sprintf arguments. Each step reads a
// smaller expression or an earlier declaration, so the bound only stops a pathological chain.
const readingDepth = 16

func run(pass *analysis.Pass) (any, error) {
	reader := queryReader{pass: pass, initializers: singleAssignmentInitializers(pass)}
	// A local read in place may reach two query calls, and its value is one mistake, reported once.
	reported := map[ast.Expr]bool{}
	for _, file := range pass.Files {
		ast.Inspect(file, func(node ast.Node) bool {
			call, isCall := node.(*ast.CallExpr)
			if !isCall {
				return true
			}
			argumentIndex, isQuery := queryArgumentIndex(pass, call)
			if !isQuery {
				return true
			}
			query := reader.read(call.Args[argumentIndex], 0)
			if len(query.holes) == 0 {
				return true
			}
			statement, certain := sqltext.ReadStatement(query.texts)
			if !certain {
				return true
			}
			for _, spanIndex := range statement.QuotedSpans {
				found := query.holes[spanIndex]
				if !reported[found.expression] && reader.canHoldQuote(found) && !reader.isEscaped(found.expression) {
					reported[found.expression] = true
					report(pass, found.expression, interpolatedSqlStringMessage)
				}
			}
			for _, spanIndex := range statement.BareSpans {
				found := query.holes[spanIndex]
				if !reported[found.expression] && reader.canHoldText(found) {
					reported[found.expression] = true
					report(pass, found.expression, interpolatedSqlValueMessage)
				}
			}
			return true
		})
	}
	return nil, nil
}

// queryArgumentIndex is the index of call's query argument when call is a query method of database/sql's *DB,
// *Tx or *Conn called as a method value, and whether it is one. A method expression, (*sql.DB).Query(database,
// query), shifts its arguments by one and is not read.
func queryArgumentIndex(pass *analysis.Pass, call *ast.CallExpr) (int, bool) {
	selector, isSelector := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !isSelector {
		return 0, false
	}
	selection, isSelection := pass.TypesInfo.Selections[selector]
	if !isSelection || selection.Kind() != types.MethodVal {
		return 0, false
	}
	method, isFunc := selection.Obj().(*types.Func)
	if !isFunc || method.Pkg() == nil || method.Pkg().Path() != "database/sql" {
		return 0, false
	}
	argumentIndex, isQueryMethod := queryArgumentIndexes[method.Name()]
	if !isQueryMethod || !isQueryReceiver(method.Signature().Recv()) {
		return 0, false
	}
	// The table is checked against the signature, so a database/sql that ever moved the query would be missed
	// rather than read at the wrong argument.
	parameters := method.Signature().Params()
	if argumentIndex >= parameters.Len() || argumentIndex >= len(call.Args) {
		return 0, false
	}
	if basic, isBasic := parameters.At(argumentIndex).Type().(*types.Basic); !isBasic || basic.Kind() != types.String {
		return 0, false
	}
	return argumentIndex, true
}

// isQueryReceiver is whether receiver is a pointer to one of database/sql's queryReceivers.
func isQueryReceiver(receiver *types.Var) bool {
	if receiver == nil {
		return false
	}
	pointer, isPointer := types.Unalias(receiver.Type()).(*types.Pointer)
	if !isPointer {
		return false
	}
	named, isNamed := types.Unalias(pointer.Elem()).(*types.Named)
	return isNamed && named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == "database/sql" &&
		queryReceivers[named.Obj().Name()]
}

// hole is a value written into a query's text.
type hole struct {
	expression ast.Expr
	// printsHex is set when fmt.Sprintf formats the value with `%x` or `%X`, which prints hex digits whatever the
	// value holds.
	printsHex bool
}

func textTemplate(text string) template {
	return template{texts: []string{text}}
}

func holeTemplate(found hole) template {
	return template{texts: []string{"", ""}, holes: []hole{found}}
}

// verb is one formatting verb of a fmt format.
type verb struct {
	letter rune
	// plain is a verb with no flag, width or precision, which prints its argument's whole text unchanged.
	plain bool
}

// readFormat splits a fmt format into the texts around its verbs, one more text than verbs, with `%%` read as
// `%`, and reports whether every verb takes the next argument in order. A `[n]` index or a `*` width or
// precision reorders or consumes arguments, and a format ending in a lone `%` has a verb with no letter, so
// neither is read.
func readFormat(format string) ([]string, []verb, bool) {
	texts := []string{""}
	verbs := []verb{}
	for index := 0; index < len(format); {
		if format[index] != '%' {
			texts[len(texts)-1] += format[index : index+1]
			index++
			continue
		}
		if index+1 < len(format) && format[index+1] == '%' {
			texts[len(texts)-1] += "%"
			index += 2
			continue
		}
		start := index + 1
		end := start
		for end < len(format) && strings.IndexByte("+-# 0123456789.", format[end]) >= 0 {
			end++
		}
		if end >= len(format) || format[end] == '[' || format[end] == '*' {
			return nil, nil, false
		}
		// A verb is one rune, as fmt reads it. One fmt does not know is still a verb that takes an argument, and
		// fmt prints that argument's text in its complaint, so it is a hole like any other.
		letter, width := utf8.DecodeRuneInString(format[end:])
		verbs = append(verbs, verb{letter: letter, plain: end == start})
		texts = append(texts, "")
		index = end + width
	}
	return texts, verbs, true
}

// isSprintf is whether call calls fmt.Sprintf, by its object, so a function of that name elsewhere never counts.
func isSprintf(pass *analysis.Pass, call *ast.CallExpr) bool {
	function, isFunc := typeutil.Callee(pass.TypesInfo, call).(*types.Func)
	return isFunc && function.Pkg() != nil && function.Pkg().Path() == "fmt" && function.Name() == "Sprintf"
}

// isString is whether candidate is a string type, a named one included.
func isString(candidate types.Type) bool {
	if candidate == nil {
		return false
	}
	basic, isBasic := candidate.Underlying().(*types.Basic)
	return isBasic && basic.Info()&types.IsString != 0
}

// canHoldText is whether a value of candidate's type, once isInert has ruled out a number or a boolean, can print
// arbitrary text: a string, an interface, whose dynamic value is unknown, or a type parameter whose type set is not
// only numbers and booleans. A struct, pointer, slice or map prints in the fmt package's form, and calling it
// quote-capable would be a guess, as the TypeScript twin declines to for an object.
func canHoldText(candidate types.Type) bool {
	if candidate == nil {
		return false
	}
	// A type parameter's Underlying is its constraint, an interface, so it is asked about before the switch.
	if _, isParameter := types.Unalias(candidate).(*types.TypeParam); isParameter {
		return true
	}
	switch underlying := candidate.Underlying().(type) {
	case *types.Basic:
		return underlying.Info()&types.IsString != 0
	case *types.Interface:
		return true
	}
	return false
}

// singleAssignmentInitializers is every local of the package assigned exactly once, by the declaration that
// gives it a value, and never through its address, each with that value. Such a local holds its initializer
// everywhere it is read, so reading the initializer in its place is exact. A package variable is left out, since
// another package may assign an exported one, and code here may reach any one through a pointer.
func singleAssignmentInitializers(pass *analysis.Pass) map[*types.Var]ast.Expr {
	writes := map[*types.Var]int{}
	initializers := map[*types.Var]ast.Expr{}
	addressTaken := map[*types.Var]bool{}
	variableOf := func(expression ast.Expr) *types.Var {
		identifier, isIdentifier := ast.Unparen(expression).(*ast.Ident)
		if !isIdentifier {
			return nil
		}
		variable, isVariable := pass.TypesInfo.ObjectOf(identifier).(*types.Var)
		if !isVariable || variable.Parent() == nil || variable.Parent() == pass.Pkg.Scope() {
			return nil
		}
		return variable
	}
	// declares is whether left is a name a := declares here, rather than one it reuses from an earlier declaration.
	declares := func(left ast.Expr) bool {
		identifier, isIdentifier := ast.Unparen(left).(*ast.Ident)
		return isIdentifier && pass.TypesInfo.Defs[identifier] != nil
	}
	for _, file := range pass.Files {
		ast.Inspect(file, func(node ast.Node) bool {
			switch statement := node.(type) {
			case *ast.AssignStmt:
				for index, left := range statement.Lhs {
					variable := variableOf(left)
					if variable == nil {
						continue
					}
					writes[variable]++
					// Only a := that declares the name gives it its value; one that reuses a name declared
					// earlier is a second write, which the count above already refuses.
					if statement.Tok == token.DEFINE && declares(left) && len(statement.Lhs) == len(statement.Rhs) {
						initializers[variable] = statement.Rhs[index]
					}
				}
			case *ast.ValueSpec:
				for index, name := range statement.Names {
					variable := variableOf(name)
					if variable == nil {
						continue
					}
					writes[variable]++
					if len(statement.Names) == len(statement.Values) {
						initializers[variable] = statement.Values[index]
					}
				}
			case *ast.RangeStmt:
				for _, assigned := range []ast.Expr{statement.Key, statement.Value} {
					if variable := variableOf(assigned); variable != nil {
						writes[variable]++
					}
				}
			case *ast.IncDecStmt:
				if variable := variableOf(statement.X); variable != nil {
					writes[variable]++
				}
			case *ast.UnaryExpr:
				if statement.Op == token.AND {
					if variable := variableOf(statement.X); variable != nil {
						addressTaken[variable] = true
					}
				}
			}
			return true
		})
	}
	single := map[*types.Var]ast.Expr{}
	for variable, initializer := range initializers {
		if writes[variable] == 1 && !addressTaken[variable] {
			single[variable] = initializer
		}
	}
	return single
}

// report flags expression with message.
func report(pass *analysis.Pass, expression ast.Expr, message policy.MessageHandle) {
	pass.Report(analysis.Diagnostic{
		Pos:      expression.Pos(),
		End:      expression.End(),
		Category: message.Id,
		Message:  message.Render(map[string]string{}),
	})
}
