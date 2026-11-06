package javascript

import (
	"strings"

	"github.com/system-inc/cohere/internal/format/doc"
	"github.com/system-inc/cohere/internal/format/estree"
	"github.com/system-inc/cohere/internal/format/printing"
	"github.com/system-inc/cohere/internal/types/source_name"
)

// print/type-parameters.js.

// Keep comma if the file extension not `.ts` and
// has one type parameter that isn't extend with any types.
// Because, otherwise formatted result will be invalid as tsx.
//
// shouldForceTrailingComma is upstream's shouldForceTrailingComma. Upstream tests the path against
// /\.ts$/; the name is read through source_name.TreatedAs, so an Adamic `.a` file drops the comma as the
// same file named `.ts` does (#6mhafvb).
func shouldForceTrailingComma(path *Path, options *Options, parametersKey string) bool {
	current := node(path)
	return len(getFunctionParameters(current)) == 1 &&
		strings.HasPrefix(current.Type(), "TS") &&
		!current.List(parametersKey)[0].Truthy("constraint") &&
		parentOf(path).Is("ArrowFunctionExpression") &&
		!(settingsOf(options).FilePath != "" &&
			strings.HasSuffix(source_name.TreatedAs(settingsOf(options).FilePath), ".ts"))
}

/*
- `GenericTypeAnnotation` (Flow)
- `TypeParameterDeclaration` (Flow)
- `TypeParameterInstantiation` (Flow)
- `TSTypeParameterDeclaration` (TypeScript)
- `TSTypeParameterInstantiation` (TypeScript)
- `TSImportType` (TypeScript)
- `TSTypeReference` (TypeScript)
*/
// printTypeParameters is upstream's printTypeParameters.
func printTypeParameters(path *Path, options *Options, print PrintFunc, parametersKey string) Doc {
	current := node(path)
	// Upstream's `node[paramsKey]` is either an array or a single node; Go reads which with a type
	// switch. An empty array is truthy upstream, so only a missing value returns "".
	value := current.Get(parametersKey)

	if value == nil {
		return emptyDoc
	}

	// for TypeParameterDeclaration typeParameters is a single node
	parameters, isArray := value.(estree.Nodes)
	if !isArray {
		return print(parametersKey, nil)
	}

	isParameterInTestCall := isTestCall(grandparentOf(path), nil)

	isArrowFunctionVariable := path.Match(
		func(value PathValue, _ string, _ int, _ bool) bool {
			node, _ := value.Node()
			return !(len(node.List(parametersKey)) == 1 && isObjectType(node.List(parametersKey)[0]))
		},
		nil,
		func(_ PathValue, name string, _ int, _ bool) bool {
			return name == "typeAnnotation"
		},
		func(value PathValue, _ string, _ int, _ bool) bool {
			node, _ := value.Node()
			return node.Is("Identifier")
		},
		func(value PathValue, _ string, _ int, _ bool) bool {
			node, _ := value.Node()
			return isArrowFunctionVariableDeclarator(node)
		},
	)

	shouldInline := len(parameters) == 0 ||
		(!isArrowFunctionVariable &&
			(isParameterInTestCall ||
				(len(parameters) == 1 &&
					(parameters[0].Is("NullableTypeAnnotation") ||
						shouldHugType(parameters[0])))) &&
			!someNode(parameters, func(node Node) bool {
				comments := getComments(
					node,
					0,
					func(comment Node) bool {
						return comment.CommentData().Leading || comment.CommentData().Trailing
					},
				)
				return len(comments) > 0 &&
					(someNode(comments, isLineComment) ||
						// This condition base on existing one in class-body.js
						// It is not really correct, but we don't have a way to check how comments are printed
						hasNewline(originalText(options), locEnd(comments[len(comments)-1])))
			}))

	if shouldInline {
		return concatIn(path, doc.Text("<"),
			join(doc.Text(", "), printAll(path, print, parametersKey)),
			printDanglingCommentsForInline(path, options),
			doc.Text(">"),
		)
	}

	var trailingComma Doc
	if current.Is("TSTypeParameterInstantiation") { // https://github.com/microsoft/TypeScript/issues/21984
		trailingComma = emptyDoc
	} else if shouldForceTrailingComma(path, options, parametersKey) {
		trailingComma = doc.Text(",")
	} else {
		trailingComma = printTrailingComma(options, "")
	}

	return groupIn(path, concatIn(path, doc.Text("<"),
		indentIn(path, concatIn(
			path,
			softline,
			join(concatIn(path, doc.Text(","), line), printAll(path, print, parametersKey)),
		)),
		trailingComma,
		softline,
		doc.Text(">"),
	))
}

// printDanglingCommentsForInline is upstream's printDanglingCommentsForInline.
func printDanglingCommentsForInline(path *Path, options *Options) Doc {
	current := node(path)
	if !hasComment(current, commentDangling, nil) {
		return emptyDoc
	}
	hasOnlyBlockComments := !hasComment(current, commentLine, nil)
	printed := printing.PrintDanglingComments(path, options, printing.DanglingOptions[Node]{
		Indent: !hasOnlyBlockComments,
	})
	if hasOnlyBlockComments {
		return printed
	}
	return concatIn(path, printed, hardline)
}

// `TSTypeParameter` and `TypeParameter`
//
// printTypeParameter is upstream's printTypeParameter.
func printTypeParameter(path *Path, options *Options, print PrintFunc) Doc {
	current := node(path)

	constKeyword := ""
	if current.Truthy("const") {
		constKeyword = "const "
	}
	parts := []Doc{textDoc(constKeyword)}

	var name Doc
	if current.Is("TSTypeParameter") {
		name = print("name", nil)
	} else {
		name = textDoc(current.String("name"))
	}

	if current.Truthy("variance") {
		parts = append(parts, print("variance", nil))
	}

	if current.Truthy("in") {
		parts = append(parts, doc.Text("in "))
	}

	if current.Truthy("out") {
		parts = append(parts, doc.Text("out "))
	}

	parts = append(parts, name)

	// Upstream's `node.bound` branch (with `usesExtendsBound` and printTypeAnnotationProperty(path,
	// print, "bound")) is dropped: `bound` is set only on Flow's TypeParameter.

	if current.Truthy("constraint") {
		groupId := newGroupId("constraint")
		parts = append(parts,
			doc.Text(" extends"),
			groupWithIn(path, indentIn(path, line), doc.GroupOptions{Id: groupId}),
			lineSuffixBoundary,
			indentIfBreak(print("constraint", nil), groupId, false),
		)
	}

	if current.Truthy("default") {
		groupId := newGroupId("default")
		parts = append(parts,
			doc.Text(" ="),
			groupWithIn(path, indentIn(path, line), doc.GroupOptions{Id: groupId}),
			lineSuffixBoundary,
			indentIfBreak(print("default", nil), groupId, false),
		)
	}

	return groupIn(path, concatIn(path, parts...))
}
