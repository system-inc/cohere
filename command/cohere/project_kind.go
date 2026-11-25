package main

// projectKind is what the project at a root is, which says what checks it. Its value is what
// `COHERE_PROJECT_KIND` names (projectKindVariable); a reader sees its label.
type projectKind string

const (
	// projectKindTypeScriptProgram is the program a tsconfig defines, checked by this binary's own pipeline.
	projectKindTypeScriptProgram projectKind = "TypeScriptProgram"

	// projectKindSwiftPackage is a Swift package, checked by cohere-swift, which this binary runs and whose records
	// it renders.
	projectKindSwiftPackage projectKind = "SwiftPackage"

	// projectKindGoModule is a workspace a go.work defines or a module a go.mod does, checked by cohere-go.
	projectKindGoModule projectKind = "GoModule"

	// projectKindFolder is a repository root no TypeScript project holds, checked by this binary's format phase
	// alone: the JSON, Markdown and the rest the house formatter reads there, which no other language formats. It
	// has no marker, since discovery adds it (see addRootFormatProject).
	projectKindFolder projectKind = "Folder"
)

// label is how a reader sees the kind, in a project's heading and label and the summary's counts: the language
// its files are written in, and Formats for the root's format-only project, the word its flag, `--formats`, uses.
func (kind projectKind) label() string {
	switch kind {
	case projectKindTypeScriptProgram:
		return "TypeScript"
	case projectKindSwiftPackage:
		return "Swift"
	case projectKindGoModule:
		return "Go"
	case projectKindFolder:
		return "Formats"
	}
	return string(kind)
}
