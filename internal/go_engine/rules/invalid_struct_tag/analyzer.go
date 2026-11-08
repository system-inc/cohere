// Package invalid_struct_tag is staticcheck's SA5008, a struct tag that does not parse, names a key twice, or holds
// a `json` or `xml` value its package would refuse or misread, ported as a check on syntax that reports through the
// kit every port of staticcheck shares.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa5008/sa5008.go, with the tag parser of
// structtag.go and the `json` grammar of jsonv2.go, itself a modified copy of Go's encoding/json/v2/field.go. It
// keeps their logic and reports their messages word for word, each at the tag's literal. The parser reads the
// literal's text between its quotes, as the original does, so a tag written as an interpreted string, its quotes
// escaped, holds no key the parser can find, and the port leaves it alone as the original does. Duplicate `choice`,
// `optional-value` and `default` keys are left alone in a package any of whose files imports
// github.com/jessevdk/go-flags, which accepts them; whether it does is read once per package, on its first struct
// type, kept in the context's state, and handed to each struct's check. The `json` grammar is not judged in encoding/json, encoding/json/v2 or their
// external tests, which test themselves; no fixture proves it, since a module's package of that path is ambiguous with
// the standard library's and never loads.
//
// The `xml` value is judged by staticcheck's fakexml, a fork of encoding/xml's field reading over go/types, and the
// field it reads is built by staticcheck's fakereflect, both borrowed in this wave as data, since wave 5 ports them,
// and a `string` option's field type is read through staticcheck's typeutil. The original hands fakexml the struct's
// field at the position of the syntax's field list, which is not the field's index when a line declares several
// names, `A, B int`: every later line's `xml` value is judged by the tag of a field above it, false findings and
// missed ones the port keeps for parity, the original's, shown by parity's `severalNames` and `severalNamesMissed`.
// It reports with no option, so a generated
// file is judged like any other. The registry still runs the original, which stays the oracle: the parity tests
// beside this file run both over the same packages, and the two differ nowhere they hold.
package invalid_struct_tag

import (
	"fmt"
	"go/ast"
	"go/types"
	"strings"

	"honnef.co/go/tools/staticcheck/fakereflect"
	"honnef.co/go/tools/staticcheck/fakexml"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// Listener is what the port hears of each package from the dispatcher: every struct type, in preorder, each judged
// with whether the package imports go-flags, which it reads once for the package, on the first struct type, and keeps
// in the context's state.
var Listener = rule.Listener{
	Nodes: []ast.Node{(*ast.StructType)(nil)},
	Node: func(context *rule.Context, node ast.Node, _ []ast.Node) {
		structure, isStructure := node.(*ast.StructType)
		if !isStructure {
			// Invariant: the dispatcher hands Node only the struct types it was asked for.
			panic("invalid_struct_tag: the dispatcher handed over a node that is not a struct type")
		}
		flags := rule.State[packageFlags](context)
		if !flags.decided {
			flags.decided, flags.importsGoFlags = true, importsGoFlags(context)
		}
		Check(context, structure, flags.importsGoFlags)
	},
}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener under the original's
// name, so the rule, its settings and every `//cohere:ignore` naming it read the same once the port takes the
// original's place.
var Analyzer = dispatch.RuleAnalyzer(
	rule.Namespace+"SA5008",
	"SA5008",
	"Flags a struct tag that does not parse, names a key twice, or holds a json or xml value its package "+
		"would refuse: staticcheck's SA5008, ported as a check on syntax.",
	&Listener,
)

// packageFlags is what the port reads once of a package, in the context's state: whether any of its files imports
// go-flags, as the original reads it before it walks anything.
type packageFlags struct {
	// decided is whether importsGoFlags has been read of the package yet.
	decided bool
	// importsGoFlags is its answer, once decided.
	importsGoFlags bool
}

// goFlagsPath is the import path of the command-line package whose tags repeat keys on purpose.
const goFlagsPath = "github.com/jessevdk/go-flags"

// importsGoFlags is whether any file of the package imports go-flags. It reads the import's text, not the package's
// imports, as the original does, so a package vendored under `GOPATH`, whose path carries the vendor tree before it,
// still counts.
func importsGoFlags(context *rule.Context) bool {
	for _, file := range context.Files {
		for _, imported := range file.Imports {
			// The text between the literal's quotes, read as the original reads it, escapes and all.
			if imported.Path.Value[1:len(imported.Path.Value)-1] == goFlagsPath {
				return true
			}
		}
	}
	return false
}

// Check judges the tag of each field of one struct type: whether it parses, whether it names a key twice, and its
// `json` and `xml` values. importsGoFlags is whether the struct's package imports go-flags.
func Check(context *rule.Context, structure *ast.StructType, importsGoFlags bool) {
	structType, isStruct := context.TypesInfo.TypeOf(structure).(*types.Struct)
	if !isStruct {
		// Invariant: the type checker records every struct type expression of a package it checked as a struct.
		panic("invalid_struct_tag: a struct type has no struct type recorded")
	}
	reflected := fakereflect.TypeAndCanAddr{Type: structType}
	for listIndex, field := range structure.Fields.List {
		if field.Tag == nil {
			continue
		}
		values, parseError := structTagValues(field.Tag.Value[1 : len(field.Tag.Value)-1])
		if parseError != nil {
			staticcheck_report.Report(context, field.Tag, fmt.Sprintf("unparseable struct tag: %s", parseError))
			continue
		}
		for key, keyValues := range values {
			if len(keyValues) > 1 {
				isGoFlagsKey := importsGoFlags && (key == "choice" || key == "optional-value" || key == "default")
				if !isGoFlagsKey {
					staticcheck_report.Report(context, field.Tag, fmt.Sprintf("duplicate struct tag %q", key))
				}
			}
			switch key {
			case "json":
				checkJsonTag(context, field, keyValues[0])
			case "xml":
				// The field at the list's position, as the original reads it, which is not this field's when a line
				// above declared several names.
				if _, xmlError := fakexml.StructFieldInfo(reflected.Field(listIndex)); xmlError != nil {
					staticcheck_report.Report(context, field.Tag, fmt.Sprintf("invalid XML tag: %s", xmlError))
				}
				checkXmlTag(context, field, keyValues[0])
			}
		}
	}
}

// checkJsonTag judges the `json` value tag of field, except in encoding/json and its version 2, which know what they
// do and test their own malformed tags.
func checkJsonTag(context *rule.Context, field *ast.Field, tag string) {
	switch context.Package.Path() {
	case "encoding/json", "encoding/json_test", "encoding/json/v2", "encoding/json/v2_test":
		return
	}
	checkJsonTagSyntax(context, field, tag)
}

// checkXmlTag judges the options of the `xml` value tag of field, after its name: each must be one encoding/xml
// knows, and none may appear twice.
func checkXmlTag(context *rule.Context, field *ast.Field, tag string) {
	counts := map[string]int{}
	for _, option := range strings.Split(tag, ",")[1:] {
		switch option {
		case "attr", "chardata", "cdata", "innerxml", "comment", "omitempty", "any":
			counts[option]++
		case "":
		default:
			staticcheck_report.Report(context, field.Tag, fmt.Sprintf("invalid XML tag: unknown option %q", option))
		}
	}
	for option, count := range counts {
		if count > 1 {
			staticcheck_report.Report(context, field.Tag, fmt.Sprintf("invalid XML tag: duplicate option %q", option))
		}
	}
}
