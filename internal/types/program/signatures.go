package program

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/compiler"
	"github.com/microsoft/TypeScript/tsc/shim/incremental"
	"github.com/microsoft/TypeScript/tsc/shim/tspath"
	"github.com/zeebo/xxh3"
)

// Export signatures: what an importer can see of a file through types.
//
// A file's signature is the hash of its declaration output, the .d.ts the compiler would write for it, and
// of the diagnostics that output carries. An edit inside a function body leaves it unchanged; an edit to
// an exported type, an inferred return type, or a JSDoc comment on an export changes it. It is the same
// definition the compiler's own incremental builder uses to decide which importers to re-check, computed
// the same way, so the two agree: measured on ahra 2026-10-02, 3,741 of 3,741 signatures computed here
// equal the ones the compiler's buildinfo stores for the same bytes.
//
// Computing one means emitting a file's declarations, which needs the checker. Every project file costs
// about 500ms on ahra, so only files whose bytes changed since their signature was recorded are computed,
// and every other file carries its recorded signature forward. A file with nothing recorded falls back to
// its version, the hash of its bytes: the compiler's own conservative default, which can only make the
// first edit to that file invalidate more than it had to.
//
// # What a signature does not cover, and the syntax hash that does
//
// Anything an importer reads off the file's syntax rather than its types. `async f(): Promise<T>` and
// `f(): Promise<T>` emit the same declaration, and so do a function whose return type was written and one
// whose return type was inferred to the same thing. A rule reaches that syntax through a symbol's
// declarations, and 128 of 164 type-aware rules can reach a declaration, measured 2026-10-02.
//
// So a file's shape is its signature and its Syntax: the hash of its text with every function body cut
// out. Modifiers, annotations, initializers, parameters, decorators and comments outside bodies are all
// in it, so a rule reading any of them off an imported declaration sees a change. What neither half
// covers is a function body's own text, and only a rule that reads into an imported function's body
// needs more than the shape. See SignatureFingerprints.

// SignatureEntry is one file's recorded shape, with the version of the bytes it was computed from.
type SignatureEntry struct {
	Version   string
	Signature string

	// Syntax is the hash of the file's text with its function bodies elided. Computed from the parse
	// alone, so it costs no checker and is filled in for any entry that lacks it, a seeded one included.
	Syntax string
}

// FileVersion is a file's version as the compiler defines it: the xxh3 hash of its text, in hex.
func FileVersion(text string) string {
	sum := xxh3.HashString128(text).Bytes()
	return hex.EncodeToString(sum[:])
}

// Signatures returns every project file's signature, keyed by file name, and how many were computed rather
// than carried forward from previous.
func (g *Graph) Signatures(ctx context.Context, previous map[string]SignatureEntry) (map[string]SignatureEntry, int) {
	projectFiles := g.ProjectFiles()
	signatures := make(map[string]SignatureEntry, len(projectFiles))
	stale := []*ast.SourceFile{}
	for _, sourceFile := range projectFiles {
		version := FileVersion(sourceFile.Text())
		if recorded, found := previous[sourceFile.FileName()]; found && recorded.Version == version && recorded.Signature != "" {
			if recorded.Syntax == "" {
				recorded.Syntax = elidedBodiesVersion(sourceFile)
			}
			signatures[sourceFile.FileName()] = recorded
			continue
		}
		// A declaration file or a JSON module has no declaration output of its own. The compiler uses the
		// version for both, and so does this. Its whole text is its shape, so its syntax is its version too.
		if sourceFile.IsDeclarationFile || ast.IsJsonSourceFile(sourceFile) {
			signatures[sourceFile.FileName()] = SignatureEntry{Version: version, Signature: version, Syntax: version}
			continue
		}
		stale = append(stale, sourceFile)
	}
	if len(stale) == 0 {
		return signatures, 0
	}

	var mutex sync.Mutex
	g.Program.Emit(ctx, compiler.EmitOptions{
		TargetSourceFiles: stale,
		EmitOnly:          compiler.EmitOnlyBuilderSignature,
		WriteFile: func(fileName string, text string, data *compiler.WriteFileData) error {
			if data == nil || data.SourceFile == nil {
				return nil
			}
			signature := declarationSignature(data.SourceFile, text, data)
			entry := SignatureEntry{
				Version:   FileVersion(data.SourceFile.Text()),
				Signature: signature,
				Syntax:    elidedBodiesVersion(data.SourceFile),
			}
			mutex.Lock()
			signatures[data.SourceFile.FileName()] = entry
			mutex.Unlock()
			return nil
		},
	})

	// A file the emit did not write for has no signature to compare, so its version stands in, which
	// can only over-invalidate.
	for _, sourceFile := range stale {
		if _, found := signatures[sourceFile.FileName()]; !found {
			version := FileVersion(sourceFile.Text())
			signatures[sourceFile.FileName()] = SignatureEntry{Version: version, Signature: version, Syntax: version}
		}
	}
	return signatures, len(stale)
}

// elidedBodiesVersion is the version of a file's text with the body of every function, method, accessor,
// constructor, arrow function and class static block cut out, outermost first. An edit inside a body
// leaves it unchanged; an edit anywhere else changes it.
func elidedBodiesVersion(sourceFile *ast.SourceFile) string {
	text := sourceFile.Text()
	var elided strings.Builder
	kept := 0
	var visit func(node *ast.Node) bool
	visit = func(node *ast.Node) bool {
		if ast.IsFunctionLikeDeclaration(node) || ast.IsClassStaticBlockDeclaration(node) {
			if body := node.Body(); body != nil {
				// A body's position includes the trivia before it, so a comment between a signature and its
				// brace is cut with the body, and an edit to it is as invisible to the shape as one inside.
				// No rule reads such a comment off an imported declaration; one that did would need the
				// content fingerprint anyway.
				elided.WriteString(text[kept:body.Pos()])
				elided.WriteString("{}")
				kept = body.End()
				return false
			}
		}
		node.ForEachChild(visit)
		return false
	}
	sourceFile.AsNode().ForEachChild(visit)
	elided.WriteString(text[kept:])
	return FileVersion(elided.String())
}

// SeedSignatures returns previous with an entry added for every project file it lacks, taken from the
// signatures the compiler's own buildinfo recorded. A first run of the table then starts from what the
// compiler already computed instead of computing every signature (about 500ms on ahra) or falling back to
// versions, which would make each file's first edit invalidate its importers once for nothing.
//
// Only real shapes are taken. An entry whose signature is its version, or that has none, says the compiler
// never computed a shape for that file, and the file is left to be computed here.
func (g *Graph) SeedSignatures(previous map[string]SignatureEntry) map[string]SignatureEntry {
	seeded := make(map[string]SignatureEntry, len(previous))
	for name, entry := range previous {
		seeded[name] = entry
	}
	if g.CompilerHost == nil || g.Config == nil {
		return seeded
	}
	buildInfoFileName := g.Config.GetBuildInfoFileName()
	if buildInfoFileName == "" {
		return seeded
	}
	buildInfo := incremental.NewBuildInfoReader(g.CompilerHost).ReadBuildInfo(g.Config)
	if buildInfo == nil || len(buildInfo.FileNames) != len(buildInfo.FileInfos) {
		return seeded
	}
	directory := tspath.GetDirectoryPath(buildInfoFileName)
	recorded := make(map[tspath.Path]SignatureEntry, len(buildInfo.FileNames))
	for index, name := range buildInfo.FileNames {
		info := buildInfo.FileInfos[index].GetFileInfo()
		if info == nil || info.Signature() == "" || info.Signature() == info.Version() {
			continue
		}
		recorded[g.pathFor(tspath.GetNormalizedAbsolutePath(name, directory))] = SignatureEntry{Version: info.Version(), Signature: info.Signature()}
	}
	for _, sourceFile := range g.ProjectFiles() {
		if _, found := seeded[sourceFile.FileName()]; found {
			continue
		}
		if entry, found := recorded[sourceFile.Path()]; found {
			seeded[sourceFile.FileName()] = entry
		}
	}
	return seeded
}

// declarationSignature is the compiler's computeSignatureWithDiagnostics, which is unexported: the
// declaration text up to any source-map comment, then each diagnostic rendered as the compiler renders it,
// hashed.
func declarationSignature(sourceFile *ast.SourceFile, text string, data *compiler.WriteFileData) string {
	var builder strings.Builder
	if data.SourceMapUrlPos != -1 {
		builder.WriteString(text[:data.SourceMapUrlPos])
	} else {
		builder.WriteString(text)
	}
	for _, diagnostic := range data.Diagnostics {
		writeSignatureDiagnostic(diagnostic, sourceFile, &builder)
	}
	return FileVersion(builder.String())
}

func writeSignatureDiagnostic(diagnostic *ast.Diagnostic, sourceFile *ast.SourceFile, builder *strings.Builder) {
	if diagnostic == nil {
		return
	}
	builder.WriteString("\n")
	if diagnostic.File() != sourceFile {
		builder.WriteString(tspath.EnsurePathIsNonModuleName(tspath.GetRelativePathFromDirectory(
			tspath.GetDirectoryPath(string(sourceFile.Path())),
			string(diagnostic.File().Path()),
			tspath.ComparePathsOptions{},
		)))
	}
	if diagnostic.File() != nil {
		fmt.Fprintf(builder, "(%d,%d): ", diagnostic.Pos(), diagnostic.Len())
	}
	builder.WriteString(diagnostic.Category().Name())
	fmt.Fprintf(builder, "%d: ", diagnostic.Code())
	builder.WriteString(string(diagnostic.MessageKey()))
	builder.WriteString("\n")
	for _, argument := range diagnostic.MessageArgs() {
		builder.WriteString(argument)
		builder.WriteString("\n")
	}
	for _, chained := range diagnostic.MessageChain() {
		writeSignatureDiagnostic(chained, sourceFile, builder)
	}
	for _, related := range diagnostic.RelatedInformation() {
		writeSignatureDiagnostic(related, sourceFile, builder)
	}
}

// SignatureFingerprints gives each project file a hash of everything its types can depend on, like
// TypeFingerprints, except that what it hashes of every OTHER file in its import closure is that file's
// signature rather than its bytes. A body edit to a file imported by half the tree changes its own
// fingerprint and nobody else's.
//
// The file's own bytes are still in its fingerprint, all of them: a rule reads its own file's syntax.
// The global component is TypeFingerprints' own, bytes and all, since a global declaration changes types
// everywhere without being imported.
//
// It is sound only for a rule that sees other files through types. A rule that reads an imported
// declaration's syntax stays on TypeFingerprints.
func (g *Graph) SignatureFingerprints(signatures map[string]SignatureEntry) map[tspath.Path][sha256.Size]byte {
	projectFiles := g.ProjectFiles()
	global, edges, contents, resolutions := g.typeGraph()

	shapes := make(map[tspath.Path][sha256.Size]byte, len(projectFiles))
	for _, sourceFile := range projectFiles {
		entry, found := signatures[sourceFile.FileName()]
		shape := entry.Signature + "\x00" + entry.Syntax
		if !found || entry.Signature == "" || entry.Syntax == "" {
			// No shape means the bytes stand in, as they would for a file never computed.
			shape = "version " + FileVersion(sourceFile.Text())
		}
		// Where its imports resolve is part of what a file shows its importers; see TypeFingerprints.
		shapes[sourceFile.Path()] = withResolutions(sha256.Sum256([]byte(shape)), resolutions[sourceFile.Path()])
	}
	components := fingerprintComponents(projectFiles, edges, shapes, global)

	fingerprints := make(map[tspath.Path][sha256.Size]byte, len(projectFiles))
	for _, sourceFile := range projectFiles {
		hash := sha256.New()
		component := components[sourceFile.Path()]
		content := contents[sourceFile.Path()]
		hash.Write(component[:])
		hash.Write(content[:])
		var sum [sha256.Size]byte
		copy(sum[:], hash.Sum(nil))
		fingerprints[sourceFile.Path()] = sum
	}
	return fingerprints
}
