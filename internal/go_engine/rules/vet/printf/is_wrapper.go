package printf

// isWrapper is the fact that a function, or a variable holding a function literal, is a print, printf or errorf
// wrapper: x/tools' printf isWrapper, the same meaning under a type of this package, so a wrapper declared in a
// dependency is found as the original finds it. Its field is exported for gob, which encodes facts.
type isWrapper struct{ Kind wrapperKind }

// AFact makes isWrapper an analysis.Fact.
func (*isWrapper) AFact() {
	// Nothing to do: analysis.Fact is a marker interface, and having the method is what it asks.
}

// String is the fact as analysistest's `// want name:"printfWrapper"` expectations name it, the original's words.
func (fact *isWrapper) String() string {
	switch fact.Kind {
	case kindPrintf:
		return "printfWrapper"
	case kindPrint:
		return "printWrapper"
	case kindErrorf:
		return "errorfWrapper"
	}
	return "unknownWrapper"
}
