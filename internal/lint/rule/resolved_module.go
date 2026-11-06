package rule

// ResolvedModule is what a rule may read of where one of its file's imports resolved.
type ResolvedModule struct {
	ResolvedFileName        string
	IsExternalLibraryImport bool
	// PackageName is the resolved package's name, or empty when the import is not a package's.
	PackageName string
}

// IsResolved reports whether the import resolved to a file.
func (resolved ResolvedModule) IsResolved() bool {
	return resolved.ResolvedFileName != ""
}
