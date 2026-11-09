package canonical_function_hash

import (
	"errors"
	"path/filepath"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/checker"
	"github.com/microsoft/TypeScript/tsc/shim/scanner"
	"github.com/system-inc/cohere/internal/duplicate_functions"
	"github.com/system-inc/cohere/internal/lint/ecmascript/high_level_intermediate_representation"
)

// FileFunctions is what one file's functions came to: each one hashed, refused, or without a body.
type FileFunctions struct {
	Functions []duplicate_functions.Function
	Refused   []duplicate_functions.Refusal
	Bodyless  int
}

// HashFile hashes every function of sourceFile against typeChecker, which must be the checker that owns the file:
// without one every name lowers as a global and the hash degrades into a textual one. root is the project root, which
// imported declarations are named relative to, so a digest does not depend on where the checkout sits.
//
// The units are the outermost functions and every class member with a body, as the IR's ForEachFunctionLike visits
// them. A function nested in another is hashed as part of it, by its own body and what it captures.
func HashFile(sourceFile *ast.SourceFile, typeChecker *checker.Checker, root string) FileFunctions {
	hasher := &fileHasher{
		root:        root,
		typeChecker: typeChecker,
		sourceFile:  sourceFile,
		memo:        map[*ast.Node]unitResult{},
		inProgress:  map[*ast.Node]bool{},
	}
	fileName := sourceFile.FileName().AsString()
	result := FileFunctions{}
	high_level_intermediate_representation.ForEachFunctionLike(sourceFile.AsNode(), func(node *ast.Node) {
		if !ast.IsFunctionLikeDeclaration(node) {
			// A signature or a function type: a type, not a function.
			return
		}
		unit := hasher.hashNode(node)
		if unit.bodyless {
			result.Bodyless++
			return
		}
		name := functionName(node)
		line := lineOf(sourceFile, node)
		if unit.refused != nil {
			result.Refused = append(result.Refused, duplicate_functions.Refusal{
				File: fileName, Line: line, Name: name, Reason: unit.refused.reason, Detail: unit.refused.detail,
			})
			return
		}
		result.Functions = append(result.Functions, duplicate_functions.Function{
			File: fileName, Line: line, Name: name, Size: unit.size, Digest: unit.digest,
		})
	})
	return result
}

// unitResult is one function's outcome: a digest and size, a refusal, or no body at all.
type unitResult struct {
	bodyless bool
	refused  *refusal
	digest   duplicate_functions.Digest
	size     int

	// dependencies are the functions still being hashed that this one read as `local:<name>`, so a digest that
	// depended on where the walk started is never memoized.
	dependencies map[*ast.Node]bool
}

// fileHasher hashes the functions of one file. A function of the same file that another calls is hashed by its body,
// memoized, so a helper and its inlined copy agree.
type fileHasher struct {
	root        string
	typeChecker *checker.Checker
	sourceFile  *ast.SourceFile
	memo        map[*ast.Node]unitResult
	inProgress  map[*ast.Node]bool
}

// hashNode lowers and hashes one function node.
func (hasher *fileHasher) hashNode(node *ast.Node) unitResult {
	if result, memoized := hasher.memo[node]; memoized {
		return result
	}
	function := high_level_intermediate_representation.Lower(node, hasher.typeChecker)
	if function == nil {
		result := unitResult{bodyless: true}
		hasher.memo[node] = result
		return result
	}
	high_level_intermediate_representation.Construct(function)
	if unsupported := unsupportedIn(function); unsupported != nil {
		result := unitResult{refused: unsupported}
		hasher.memo[node] = result
		return result
	}
	hasher.inProgress[node] = true
	walker := newFunctionHasher(hasher, function)
	digest, hashError := walker.hashFunction()
	delete(hasher.inProgress, node)
	delete(walker.dependencies, node)
	result := unitResult{dependencies: walker.dependencies}
	if hashError != nil {
		result.refused = asRefusal(hashError)
	} else {
		result.digest, result.size = digest, walker.size
	}
	if len(result.dependencies) == 0 {
		hasher.memo[node] = result
	}
	return result
}

// asRefusal is the refusal an error from the walk carries, or one naming the error when it carries none.
func asRefusal(walkError error) *refusal {
	var refused *refusal
	if errors.As(walkError, &refused) {
		return refused
	}
	return &refusal{reason: duplicate_functions.RefusalNotHashable, detail: walkError.Error()}
}

// unsupportedIn is the refusal for the first construct lowering did not model, in the function or any function nested
// in it, or nil.
func unsupportedIn(function *high_level_intermediate_representation.Function) *refusal {
	for _, block := range function.Blocks {
		for _, instructionId := range block.Instructions {
			unsupported, isUnsupported := function.Instruction(
				instructionId,
			).Value.(*high_level_intermediate_representation.UnsupportedNode)
			if !isUnsupported {
				continue
			}
			if unsupported.Reason == ast.KindImportKeyword.String() {
				return &refusal{reason: duplicate_functions.RefusalDynamicImport}
			}
			return &refusal{reason: duplicate_functions.RefusalUnsupportedNode, detail: unsupported.Reason}
		}
		if _, isUnsupported := block.Terminal.(*high_level_intermediate_representation.Unsupported); isUnsupported {
			return &refusal{reason: duplicate_functions.RefusalUnsupportedNode, detail: "terminal"}
		}
	}
	for _, nested := range function.Functions {
		if unsupported := unsupportedIn(nested); unsupported != nil {
			return unsupported
		}
	}
	return nil
}

// global hashes a read of a name the function does not declare, with memory, the effect chain at the read, for a
// binding that can change.
//
// Mutates: dependencies gains every function still being hashed that the read reached, so the caller memoizes nothing
// that depended on where the walk started
func (hasher *fileHasher) global(
	node *ast.Node,
	name string,
	kind high_level_intermediate_representation.GlobalBindingKind,
	memory duplicate_functions.Digest,
	dependencies map[*ast.Node]bool,
) duplicate_functions.Digest {
	if name == "this" && (node == nil || node.Kind == ast.KindThisKeyword) {
		return duplicate_functions.NewDigestBuilder("this").Sum()
	}
	identity, mutable, functionNode := hasher.globalIdentity(node, name, kind)
	if functionNode != nil {
		if hasher.inProgress[functionNode] {
			dependencies[functionNode] = true
			return duplicate_functions.NewDigestBuilder("local").Text(name).Sum()
		}
		result := hasher.hashNode(functionNode)
		for dependency := range result.dependencies {
			dependencies[dependency] = true
		}
		if !result.bodyless && result.refused == nil {
			return duplicate_functions.NewDigestBuilder("local-function").Digest(result.digest).Sum()
		}
	}
	builder := duplicate_functions.NewDigestBuilder("global").Text(identity)
	if mutable {
		builder.Digest(memory)
	}
	return builder.Sum()
}

// globalIdentity names a free binding by its resolved declaration: an import as its declaring file, relative to the
// root, and name, after following aliases, never the specifier as written; a true global by name. It says whether
// the binding can change between reads, and for a function declared in this file returns its node, so the caller can
// hash it by its body.
func (hasher *fileHasher) globalIdentity(
	node *ast.Node,
	name string,
	kind high_level_intermediate_representation.GlobalBindingKind,
) (identity string, mutable bool, functionNode *ast.Node) {
	if node == nil || hasher.typeChecker == nil || node.Kind != ast.KindIdentifier {
		return "name:" + name, true, nil
	}
	symbol := hasher.typeChecker.GetSymbolAtLocation(node)
	if symbol == nil {
		return "global:" + name, true, nil
	}
	resolved := symbol
	if symbol.Flags&ast.SymbolFlagsAlias != 0 {
		resolved = checker.SkipAlias(symbol, hasher.typeChecker)
	}
	if resolved == nil || len(resolved.Declarations) == 0 ||
		kind == high_level_intermediate_representation.GlobalBindingKindGlobal {
		return "global:" + name, true, nil
	}
	declaration := resolved.Declarations[0]
	if resolved.ValueDeclaration != nil {
		declaration = resolved.ValueDeclaration
	}
	declarationFile := ast.GetSourceFileOfNode(declaration)
	fileName := "?"
	if declarationFile != nil {
		fileName = hasher.relative(declarationFile.FileName().AsString())
	}
	identity = "symbol:" + fileName + "#" + resolved.Name
	unchanging := ast.SymbolFlagsFunction | ast.SymbolFlagsClass | ast.SymbolFlagsEnum | ast.SymbolFlagsValueModule
	mutable = kind == high_level_intermediate_representation.GlobalBindingKindModuleLocal &&
		resolved.Flags&unchanging == 0
	if declaration.Kind == ast.KindVariableDeclaration && ast.IsVarConst(declaration) {
		mutable = false
	}
	if kind == high_level_intermediate_representation.GlobalBindingKindModuleLocal &&
		declarationFile == hasher.sourceFile {
		switch declaration.Kind {
		case ast.KindFunctionDeclaration:
			functionNode = declaration
		case ast.KindVariableDeclaration:
			initializer := declaration.AsVariableDeclaration().Initializer
			if !mutable && initializer != nil &&
				(initializer.Kind == ast.KindArrowFunction || initializer.Kind == ast.KindFunctionExpression) {
				functionNode = initializer
			}
		}
	}
	return identity, mutable, functionNode
}

// relative is a declaring file's name from the root, slash-separated, or the name as given when it is not below it.
func (hasher *fileHasher) relative(fileName string) string {
	relative, relativeError := filepath.Rel(hasher.root, fileName)
	if relativeError != nil {
		return fileName
	}
	return filepath.ToSlash(relative)
}

// lineOf is the 1-based line a function starts on, past its leading comments.
func lineOf(sourceFile *ast.SourceFile, node *ast.Node) int {
	position := scanner.GetTokenPosOfNode(node, sourceFile, false)
	line, _ := scanner.GetLineAndCharacterOfPosition(sourceFile, position)
	return line + 1
}
