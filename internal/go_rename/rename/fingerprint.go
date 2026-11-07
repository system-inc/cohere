package rename

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// IdentifierRecord is one identifier in a file, in source order, with the declaration it resolves to.
type IdentifierRecord struct {
	Name string
	Key  string
}

// Fingerprint records, for every file of pkg, each identifier in source order and the declaration it
// resolves to. A declaration in the same package is keyed by the ordinal of its declaring identifier in its
// file, which a rename cannot move; anything else by its package path and description. Two fingerprints of a
// file taken before and after a rename must have the same keys in the same order, and names that differ
// only where an edit was made.
func Fingerprint(
	fileSet *token.FileSet,
	packageTypes *types.Package,
	info *types.Info,
	files []*ast.File,
) map[string][]IdentifierRecord {
	ordinals := map[token.Pos]string{}
	identifiersByFile := map[string][]*ast.Ident{}
	for _, file := range files {
		filename := fileSet.PositionFor(file.Pos(), false).Filename
		ordinal := 0
		ast.Inspect(file, func(node ast.Node) bool {
			if identifier, isIdentifier := node.(*ast.Ident); isIdentifier {
				ordinals[identifier.Pos()] = fmt.Sprintf("%s#%d", filename, ordinal)
				identifiersByFile[filename] = append(identifiersByFile[filename], identifier)
				ordinal++
			}
			return true
		})
	}
	key := func(object types.Object) string {
		if object == nil {
			return "-"
		}
		if object.Pkg() == nil {
			return "universe:" + object.Name()
		}
		if object.Pkg() == packageTypes {
			if ordinal, found := ordinals[object.Pos()]; found {
				return "here:" + ordinal
			}
			return "here:" + describe(fileSet, object)
		}
		return "there:" + object.Pkg().Path() + ":" + describe(fileSet, object)
	}
	fingerprint := map[string][]IdentifierRecord{}
	for filename, identifiers := range identifiersByFile {
		records := make([]IdentifierRecord, 0, len(identifiers))
		for _, identifier := range identifiers {
			records = append(records, IdentifierRecord{
				Name: identifier.Name,
				Key:  key(info.Defs[identifier]) + "|" + key(info.Uses[identifier]),
			})
		}
		fingerprint[filename] = records
	}
	return fingerprint
}

// Insertion is a declaration a rewrite adds, `var Name TypeName` on a line of its own before Offset (a line's
// start) in Filename, where TypeName is a predeclared type. It adds two identifiers: Name, which declares,
// and TypeName, which reads the universe.
type Insertion struct {
	Filename string
	Offset   int
	Name     string
	TypeName string
}

// InsertedDeclaration is the retarget value that names an Insertion's declaration: the negative of its
// offset, less one, which no identifier's offset can be.
func InsertedDeclaration(offset int) int { return -offset - 1 }

// Expect turns a fingerprint taken before a rewrite into the one the rewrite must leave behind. An identifier
// at a key of newNames takes its new name. An identifier at a key of retargets resolves, after the rewrite, to
// a new local declared by the identifier at the offset it maps to, in the same file: a pass that splits one
// variable into several maps each new variable's occurrences to its declaring identifier, and that
// identifier to itself, which becomes a definition. A retarget to InsertedDeclaration(offset) resolves to the
// declaration inserted there under the identifier's new name. Each insertion adds its two identifiers, and
// moves the ordinal of every identifier after it, wherever a key names one. offsets is IdentifierOffsets of
// the same files.
//
// Mutates: fingerprint the fingerprint taken before, turned in place into the one expected after
func Expect(
	fingerprint map[string][]IdentifierRecord,
	offsets map[string][]int,
	newNames map[EditKey]string,
	retargets map[EditKey]int,
	insertions []Insertion,
) {
	insertedIn := map[string][]Insertion{}
	for _, insertion := range insertions {
		insertedIn[insertion.Filename] = append(insertedIn[insertion.Filename], insertion)
	}
	for filename := range insertedIn {
		sort.SliceStable(insertedIn[filename], func(left, right int) bool {
			return insertedIn[filename][left].Offset < insertedIn[filename][right].Offset
		})
	}
	// shifted is an original ordinal's place once every insertion of its file is made.
	shifted := func(filename string, ordinal int) int {
		moved := ordinal
		for _, insertion := range insertedIn[filename] {
			if ordinal < len(offsets[filename]) && offsets[filename][ordinal] >= insertion.Offset {
				moved += 2
			}
		}
		return moved
	}
	// insertedOrdinal is the ordinal of an insertion's declaring identifier.
	insertedOrdinal := func(filename string, which int) int {
		insertion := insertedIn[filename][which]
		before := sort.SearchInts(offsets[filename], insertion.Offset)
		return before + 2*which
	}
	respell := func(key string) string {
		parts := strings.Split(key, "|")
		for index, part := range parts {
			filenameAndOrdinal, isHere := strings.CutPrefix(part, "here:")
			hash := strings.LastIndexByte(filenameAndOrdinal, '#')
			if !isHere || hash < 0 || len(insertedIn[filenameAndOrdinal[:hash]]) == 0 {
				continue
			}
			ordinal, parseError := strconv.Atoi(filenameAndOrdinal[hash+1:])
			if parseError != nil {
				continue
			}
			parts[index] = fmt.Sprintf(
				"here:%s#%d",
				filenameAndOrdinal[:hash],
				shifted(filenameAndOrdinal[:hash], ordinal),
			)
		}
		return strings.Join(parts, "|")
	}
	for _, records := range fingerprint {
		for index := range records {
			records[index].Key = respell(records[index].Key)
		}
	}
	for filename, records := range fingerprint {
		ordinalAt := map[int]int{}
		for ordinal, offset := range offsets[filename] {
			ordinalAt[offset] = ordinal
		}
		for index := range records {
			key := EditKey{Filename: filename, Offset: offsets[filename][index]}
			if newName, renamed := newNames[key]; renamed {
				records[index].Name = newName
			}
			declaration, retargeted := retargets[key]
			if !retargeted {
				continue
			}
			// A declaration that is not an identifier of this file can never match, which is the answer wanted.
			declared := fmt.Sprintf("here:%s#missing-%d", filename, declaration)
			if ordinal, found := ordinalAt[declaration]; found {
				declared = fmt.Sprintf("here:%s#%d", filename, shifted(filename, ordinal))
			}
			for which, insertion := range insertedIn[filename] {
				if declaration < 0 &&
					InsertedDeclaration(insertion.Offset) == declaration &&
					insertion.Name == records[index].Name {
					declared = fmt.Sprintf("here:%s#%d", filename, insertedOrdinal(filename, which))
				}
			}
			if declaration == key.Offset {
				records[index].Key = declared + "|-"
			} else {
				records[index].Key = "-|" + declared
			}
		}
		for which := len(insertedIn[filename]) - 1; which >= 0; which-- {
			insertion := insertedIn[filename][which]
			at := sort.SearchInts(offsets[filename], insertion.Offset)
			added := []IdentifierRecord{
				{Name: insertion.Name, Key: fmt.Sprintf("here:%s#%d|-", filename, insertedOrdinal(filename, which))},
				{Name: insertion.TypeName, Key: "-|universe:" + insertion.TypeName},
			}
			records = append(records[:at], append(added, records[at:]...)...)
		}
		fingerprint[filename] = records
	}
}

// IdentifierOffsets lists, per file, the byte offset of each identifier in the same order Fingerprint uses,
// so the expected names after a rename can be derived from the edits.
func IdentifierOffsets(fileSet *token.FileSet, files []*ast.File) map[string][]int {
	offsets := map[string][]int{}
	for _, file := range files {
		filename := fileSet.PositionFor(file.Pos(), false).Filename
		ast.Inspect(file, func(node ast.Node) bool {
			if identifier, isIdentifier := node.(*ast.Ident); isIdentifier {
				offsets[filename] = append(offsets[filename], fileSet.PositionFor(identifier.Pos(), false).Offset)
			}
			return true
		})
	}
	return offsets
}

// describe names an object by kind, owner, name and declaring file, never by its line or its type. A line is
// not stable: formatting a rewritten file can move every declaration below the edit (a two-line shift read
// as 229 mismatches on a tree whose renames were sound). A type is not stable either: a function's type
// spells its parameter names, which a rename changes. The renames these passes make are locals, which never
// cross a package, so an object reached from another package keeps its name, owner and file.
func describe(fileSet *token.FileSet, object types.Object) string {
	// Adjusted by //line directives: a cgo package is read from cgo's translation in the build cache by one load and
	// from its source by another, and the translation names its source file in those directives.
	position := fileSet.PositionFor(object.Pos(), true)
	owner := ""
	if function, isFunction := object.(*types.Func); isFunction {
		if signature, isSignature := function.Type().(*types.Signature); isSignature && signature.Recv() != nil {
			owner = types.TypeString(signature.Recv().Type(), func(*types.Package) string { return "" }) + "."
		}
	}
	// The file's base name only: export data and a source load spell the same file's path differently
	// (module-relative against absolute), and the package path beside it already says which file it is.
	return fmt.Sprintf("%s %s%s @%s", objectKind(object), owner, object.Name(), filepath.Base(position.Filename))
}
