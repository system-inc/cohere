package rule

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/core"
	"github.com/microsoft/TypeScript/tsc/shim/parser"
	"github.com/microsoft/TypeScript/tsc/shim/scanner"
	"github.com/microsoft/TypeScript/tsc/shim/tspath"
)

// corpusDirectories are the fixtures TestTokenRangeStartsWhereTheScannerDoes reads, from the module's root. It
// used to read every TypeScript and JavaScript file under the root, and Go's test cache keys a result on every
// file and directory listing a test opens, so an edit anywhere in cohere, a document included, reran rule's tests
// on every landing (#0zdyk51). What the comparison needs is every kind of node, not every file: on 2026-11-27 the
// walk of the whole module compared 683,942 nodes in 868 files and reached the 199 kinds of wholeModuleKinds.
// These directories reach all 199 in 740 files and 589,732 nodes. The two largest fixture corpora, React's
// conformance fixtures and Adamic's, reach 176; the text rules' conformance corpus adds `override`, the
// intermediate representation's lowering fixture `satisfies`, a non-null assertion, a static block and a named
// tuple member, and the function hash's pairs a bigint and `public`. The rest no fixture writes, and two
// sources cohere ships rather than tests give them: Prettier's vendored plugins, minified, the compound and shift
// assignments, `&`, `in`, `super`, `debugger` and the empty statement, and the Tailwind planted repository's
// generator, the one file of the module that writes `^` and `^=`. Both change only when what they vendor or
// generate does.
var corpusDirectories = []string{
	"internal/lint/rules/react/conformance/testdata",
	"internal/lint/rules/adamic/testdata",
	"internal/text_rules/conformance/testdata",
	"internal/lint/ecmascript/high_level_intermediate_representation/testdata",
	"internal/lint/ecmascript/canonical_function_hash/testdata",
	"internal/format/prettier/bundles/plugins",
	"internal/lint/rules/tailwind/tools/generate_planted_repository",
}

// wholeModuleKinds are the kinds of node the walk of every TypeScript and JavaScript file of the module reached on
// 2026-11-27, JSDoc included. corpusDirectories must reach each, so narrowing the corpus lost no shape.
var wholeModuleKinds = []ast.Kind{
	ast.KindAmpersandAmpersandToken, ast.KindAmpersandEqualsToken, ast.KindAmpersandToken, ast.KindAnyKeyword,
	ast.KindArrayBindingPattern, ast.KindArrayLiteralExpression, ast.KindArrayType, ast.KindArrowFunction,
	ast.KindAsExpression, ast.KindAsteriskAsteriskToken, ast.KindAsteriskEqualsToken, ast.KindAsteriskToken,
	ast.KindAsyncKeyword, ast.KindAwaitExpression, ast.KindAwaitKeyword, ast.KindBarBarToken, ast.KindBarEqualsToken,
	ast.KindBarToken, ast.KindBigIntKeyword, ast.KindBigIntLiteral, ast.KindBinaryExpression, ast.KindBindingElement,
	ast.KindBlock, ast.KindBooleanKeyword, ast.KindBreakStatement, ast.KindCallExpression, ast.KindCallSignature,
	ast.KindCaretEqualsToken, ast.KindCaretToken, ast.KindCaseBlock, ast.KindCaseClause, ast.KindCatchClause,
	ast.KindClassDeclaration, ast.KindClassExpression, ast.KindClassStaticBlockDeclaration, ast.KindColonToken,
	ast.KindCommaToken, ast.KindComputedPropertyName, ast.KindConditionalExpression, ast.KindConstKeyword,
	ast.KindConstructor, ast.KindContinueStatement, ast.KindDebuggerStatement, ast.KindDeclareKeyword,
	ast.KindDefaultClause, ast.KindDefaultKeyword, ast.KindDeleteExpression, ast.KindDoStatement,
	ast.KindDotDotDotToken, ast.KindElementAccessExpression, ast.KindEmptyStatement, ast.KindEndOfFile,
	ast.KindEnumDeclaration, ast.KindEnumMember, ast.KindEqualsEqualsEqualsToken, ast.KindEqualsEqualsToken,
	ast.KindEqualsGreaterThanToken, ast.KindEqualsToken, ast.KindExclamationEqualsEqualsToken,
	ast.KindExclamationEqualsToken, ast.KindExportAssignment, ast.KindExportDeclaration, ast.KindExportKeyword,
	ast.KindExportSpecifier, ast.KindExpressionStatement, ast.KindExpressionWithTypeArguments, ast.KindFalseKeyword,
	ast.KindForInStatement, ast.KindForOfStatement, ast.KindForStatement, ast.KindFunctionDeclaration,
	ast.KindFunctionExpression, ast.KindFunctionType, ast.KindGetAccessor, ast.KindGreaterThanEqualsToken,
	ast.KindGreaterThanGreaterThanEqualsToken, ast.KindGreaterThanGreaterThanGreaterThanToken,
	ast.KindGreaterThanGreaterThanToken, ast.KindGreaterThanToken, ast.KindHeritageClause, ast.KindIdentifier,
	ast.KindIfStatement, ast.KindImportClause, ast.KindImportDeclaration, ast.KindImportEqualsDeclaration,
	ast.KindImportKeyword, ast.KindImportSpecifier, ast.KindInKeyword, ast.KindIndexedAccessType,
	ast.KindInstanceOfKeyword, ast.KindInterfaceDeclaration, ast.KindIntersectionType, ast.KindJSDoc,
	ast.KindJSDocNullableType, ast.KindJSDocText, ast.KindJSDocUnknownTag, ast.KindJsxAttribute, ast.KindJsxAttributes,
	ast.KindJsxClosingElement, ast.KindJsxClosingFragment, ast.KindJsxElement, ast.KindJsxExpression,
	ast.KindJsxFragment, ast.KindJsxNamespacedName, ast.KindJsxOpeningElement, ast.KindJsxOpeningFragment,
	ast.KindJsxSelfClosingElement, ast.KindJsxSpreadAttribute, ast.KindJsxText, ast.KindLabeledStatement,
	ast.KindLessThanEqualsToken, ast.KindLessThanLessThanToken, ast.KindLessThanToken, ast.KindLiteralType,
	ast.KindMetaProperty, ast.KindMethodDeclaration, ast.KindMethodSignature, ast.KindMinusEqualsToken,
	ast.KindMinusToken, ast.KindModuleBlock, ast.KindModuleDeclaration, ast.KindNamedExports, ast.KindNamedImports,
	ast.KindNamedTupleMember, ast.KindNamespaceImport, ast.KindNewExpression, ast.KindNoSubstitutionTemplateLiteral,
	ast.KindNonNullExpression, ast.KindNullKeyword, ast.KindNumberKeyword, ast.KindNumericLiteral,
	ast.KindObjectBindingPattern, ast.KindObjectKeyword, ast.KindObjectLiteralExpression, ast.KindOmittedExpression,
	ast.KindOverrideKeyword, ast.KindParameter, ast.KindParenthesizedExpression, ast.KindParenthesizedType,
	ast.KindPercentToken, ast.KindPlusEqualsToken, ast.KindPlusToken, ast.KindPostfixUnaryExpression,
	ast.KindPrefixUnaryExpression, ast.KindPrivateIdentifier, ast.KindPrivateKeyword, ast.KindPropertyAccessExpression,
	ast.KindPropertyAssignment, ast.KindPropertyDeclaration, ast.KindPropertySignature, ast.KindPublicKeyword,
	ast.KindQualifiedName, ast.KindQuestionDotToken, ast.KindQuestionQuestionEqualsToken, ast.KindQuestionQuestionToken,
	ast.KindQuestionToken, ast.KindReadonlyKeyword, ast.KindRegularExpressionLiteral, ast.KindReturnStatement,
	ast.KindSatisfiesExpression, ast.KindSetAccessor, ast.KindShorthandPropertyAssignment, ast.KindSlashToken,
	ast.KindSpreadAssignment, ast.KindSpreadElement, ast.KindStaticKeyword, ast.KindStringKeyword,
	ast.KindStringLiteral, ast.KindSuperKeyword, ast.KindSwitchStatement, ast.KindSymbolKeyword,
	ast.KindTaggedTemplateExpression, ast.KindTemplateExpression, ast.KindTemplateHead, ast.KindTemplateMiddle,
	ast.KindTemplateSpan, ast.KindTemplateTail, ast.KindThisKeyword, ast.KindThrowStatement, ast.KindTrueKeyword,
	ast.KindTryStatement, ast.KindTupleType, ast.KindTypeAliasDeclaration, ast.KindTypeLiteral,
	ast.KindTypeOfExpression, ast.KindTypeOperator, ast.KindTypeParameter, ast.KindTypeQuery, ast.KindTypeReference,
	ast.KindUndefinedKeyword, ast.KindUnionType, ast.KindUnknownKeyword, ast.KindVariableDeclaration,
	ast.KindVariableDeclarationList, ast.KindVariableStatement, ast.KindVoidExpression, ast.KindVoidKeyword,
	ast.KindWhileStatement, ast.KindYieldExpression,
}

// TokenRange finds a node's first token with the compiler's trivia skip, where it used to build a scanner per
// call (scanner.GetRangeOfTokenAtPosition): 252 MB and 1.5M objects of a cold ahra run (#9xfg09f).
// It keeps only the start, so the two agree exactly when they agree on every node's start. Findings identical
// on four consumers would only show that no current rule reached a shape where they differ, so this compares
// them on every node of every file of corpusDirectories, JSX, templates and JSDoc among them, which between them
// reach every kind of node the whole module does. A start one past the scanner's is the mutant it must catch. The
// scanner's start is clamped to the node's end, since a range never starts past its end; see TokenRange.
func TestTokenRangeStartsWhereTheScannerDoes(test *testing.T) {
	test.Parallel()
	root := repositoryRoot(test)
	var files []*ast.SourceFile
	for _, directory := range corpusDirectories {
		read := corpusFiles(test, filepath.Join(root, filepath.FromSlash(directory)))
		// A directory moved or renamed reads as empty, and the others might still reach every kind.
		if len(read) == 0 {
			test.Errorf("%s holds no TypeScript or JavaScript file: corpusDirectories names where it went", directory)
		}
		files = append(files, read...)
	}
	// 740 files on 2026-11-27. The floor sits under that so a trimmed fixture is no failure, while a corpus read
	// from the wrong root, or one that lost a directory's worth of files, is.
	if len(files) < 700 {
		test.Fatalf("the corpus holds %d files, too few for agreement to mean anything", len(files))
	}

	// The scanner's start, never past the node's end: a zero-width node has no token, and the scanner's next one
	// lies past the trivia after it (#p86b9pw). How many nodes that clamp touches is logged by kind below.
	scannerStart := func(sourceFile *ast.SourceFile, node *ast.Node) int {
		return min(scanner.GetRangeOfTokenAtPosition(sourceFile, node.Pos()).Pos(), node.End())
	}
	_, clamped, _ := compareStarts(files, scannerStart, func(sourceFile *ast.SourceFile, node *ast.Node) int {
		return scanner.GetRangeOfTokenAtPosition(sourceFile, node.Pos()).Pos()
	})
	test.Logf("the scanner's start passes the node's end on %d nodes, by kind: %s", total(clamped), summary(clamped))
	nodes, disagreements, visited := compareStarts(files, scannerStart, func(
		sourceFile *ast.SourceFile,
		node *ast.Node,
	) int {
		return TokenRange(sourceFile, node).Pos()
	})
	var unreached []string
	for _, kind := range wholeModuleKinds {
		if !visited[kind] {
			unreached = append(unreached, kind.String())
		}
	}
	if len(unreached) > 0 {
		test.Errorf("the corpus reaches no %s, which the whole module did: a fixture lost the shape, and "+
			"corpusDirectories needs a directory that has it", strings.Join(unreached, ", "))
	}
	if len(disagreements) > 0 {
		test.Errorf(
			"TokenRange and the scanner disagree on %d of %d nodes, by kind: %s",
			total(disagreements),
			nodes,
			summary(disagreements),
		)
	}
	test.Logf("%d nodes in %d files compared, %d disagreeing", nodes, len(files), total(disagreements))

	_, caught, _ := compareStarts(files, scannerStart, func(sourceFile *ast.SourceFile, node *ast.Node) int {
		return TokenRange(sourceFile, node).Pos() + 1
	})
	if total(caught) == 0 {
		test.Error("a start one past the scanner's agreed on every node, so this comparison could not have failed")
	}
}

// repositoryRoot is the module's root, where the fixtures are.
func repositoryRoot(test *testing.T) string {
	test.Helper()
	directory, workingDirectoryError := os.Getwd()
	if workingDirectoryError != nil {
		test.Fatal(workingDirectoryError)
	}
	for {
		if _, statError := os.Stat(filepath.Join(directory, "go.mod")); statError == nil {
			return directory
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			test.Fatal("no go.mod above the test's directory")
		}
		directory = parent
	}
}

// corpusFiles is every TypeScript and JavaScript source under root, outside dependency, build and cache
// directories, each parsed.
func corpusFiles(test *testing.T, root string) []*ast.SourceFile {
	test.Helper()
	kinds := map[string]core.ScriptKind{".ts": core.ScriptKindTS, ".mts": core.ScriptKindTS, ".cts": core.ScriptKindTS,
		".tsx": core.ScriptKindTSX, ".js": core.ScriptKindJS, ".jsx": core.ScriptKindJSX, ".mjs": core.ScriptKindJS}
	skipped := map[string]bool{
		"node_modules": true,
		".git":         true,
		".cache":       true,
		".next":        true,
		"dist":         true,
	}
	var files []*ast.SourceFile
	// Ignored: the callback skips every path it cannot read and never returns an error, so the walk has
	// none to return.
	filepath.WalkDir(root, func(path string, entry fs.DirEntry, pathError error) error {
		if pathError != nil {
			return nil
		}
		if entry.IsDir() {
			if path != root && skipped[entry.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		kind, handled := kinds[strings.ToLower(filepath.Ext(path))]
		if !handled || entry.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		contents, readError := os.ReadFile(path)
		if readError != nil {
			return nil
		}
		fileName := tspath.NormalizePath(path)
		if sourceFile := parser.ParseSourceFile(ast.SourceFileParseOptions{
			FileName: tspath.RootedFilePath(fileName),
			PathKey:  tspath.PathKey(fileName),
		},
			string(contents), kind); sourceFile != nil {
			files = append(files, sourceFile)
		}
		return nil
	})
	return files
}

// compareStarts counts the nodes of files, JSDoc included, and the kinds of those whose starts differ, and says
// which kinds it visited.
func compareStarts(files []*ast.SourceFile, expected func(*ast.SourceFile, *ast.Node) int,
	actual func(*ast.SourceFile, *ast.Node) int) (int, map[string]int, map[ast.Kind]bool) {
	nodes := 0
	disagreements := map[string]int{}
	visited := map[ast.Kind]bool{}
	for _, sourceFile := range files {
		var visit func(node *ast.Node) bool
		visit = func(node *ast.Node) bool {
			nodes++
			visited[node.Kind] = true
			if expected(sourceFile, node) != actual(sourceFile, node) {
				disagreements[node.Kind.String()]++
			}
			for _, documentation := range node.JSDoc(sourceFile) {
				visit(documentation)
			}
			node.ForEachChild(visit)
			return false
		}
		sourceFile.AsNode().ForEachChild(visit)
	}
	return nodes, disagreements, visited
}

func total(counts map[string]int) int {
	sum := 0
	for _, count := range counts {
		sum += count
	}
	return sum
}

func summary(counts map[string]int) string {
	kinds := make([]string, 0, len(counts))
	for kind, count := range counts {
		kinds = append(kinds, fmt.Sprintf("%s %d", kind, count))
	}
	sort.Strings(kinds)
	return strings.Join(kinds, ", ")
}
