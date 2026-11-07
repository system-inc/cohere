package global_rename

import (
	"fmt"

	"github.com/system-inc/cohere/internal/go_rename/rename"
)

// MismatchKind is what a Mismatch found differing.
type MismatchKind string

const (
	// MismatchKindPackageGone is a root package the expectation holds and the load of the rewrite does not.
	MismatchKindPackageGone MismatchKind = "PackageGone"
	// MismatchKindPackageNew is a root package the load of the rewrite holds and the expectation does not.
	MismatchKindPackageNew MismatchKind = "PackageNew"
	// MismatchKindFileCount is a package whose files differ in number.
	MismatchKindFileCount MismatchKind = "FileCount"
	// MismatchKindIdentifierCount is a file whose identifiers differ in number, or a file gone, which has none.
	MismatchKindIdentifierCount MismatchKind = "IdentifierCount"
	// MismatchKindIdentifier is one identifier under another name, or resolving to another declaration, than
	// expected.
	MismatchKindIdentifier MismatchKind = "Identifier"
	// MismatchKindPlatformLoad is a platform the tree does not load under, before the rewrite or with it, so nothing
	// was compared there.
	MismatchKindPlatformLoad MismatchKind = "PlatformLoad"
	// MismatchKindPlatformPackage is a root package that type-checks under a platform before the rewrite and not
	// after.
	MismatchKindPlatformPackage MismatchKind = "PlatformPackage"
)

// Mismatch is one place a load of the rewrite reads back other than its Expectation, as data, so a caller can tell
// which renames it touches: the package, the file, and for an identifier, which one and what it was expected to
// name and named.
type Mismatch struct {
	Kind MismatchKind
	// Package is the root package's ID.
	Package string
	// Filename is the file's absolute path, for an IdentifierCount or an Identifier mismatch.
	Filename string
	// Index is an Identifier mismatch's place among its file's identifiers in source order, and Offset its byte
	// offset in the file before the rewrite: with Filename, the rename.EditKey of an edit made there.
	Index  int
	Offset int
	// Expected and Actual are an Identifier mismatch's record as expected and as read back. A record's Key names
	// the declarations the identifier defines and uses.
	Expected rename.IdentifierRecord
	Actual   rename.IdentifierRecord
	// ExpectedCount and ActualCount are, before and after, the identifiers in the file for an IdentifierCount
	// mismatch, and the files in the package for a FileCount mismatch.
	ExpectedCount int
	ActualCount   int
	// Platform is the platform a mismatch was found under, as `GOOS/GOARCH`, and empty for the host's load.
	Platform string
	// Reason is why a PlatformLoad or PlatformPackage mismatch failed to load or type-check.
	Reason string
}

// String is the mismatch as Apply's report spells it.
func (mismatch Mismatch) String() string {
	switch mismatch.Kind {
	case MismatchKindPlatformLoad:
		return fmt.Sprintf("the tree does not load as %s: %s", mismatch.Platform, mismatch.Reason)
	case MismatchKindPlatformPackage:
		return fmt.Sprintf(
			"package %s type-checks as %s before the rewrite and not after: %s",
			mismatch.Package,
			mismatch.Platform,
			mismatch.Reason,
		)
	case MismatchKindPackageGone:
		return fmt.Sprintf("package %s is gone after the rewrite", mismatch.Package)
	case MismatchKindPackageNew:
		return fmt.Sprintf("package %s is new after the rewrite", mismatch.Package)
	case MismatchKindFileCount:
		return fmt.Sprintf(
			"%s has %d files before, %d after",
			mismatch.Package,
			mismatch.ExpectedCount,
			mismatch.ActualCount,
		)
	case MismatchKindIdentifierCount:
		return fmt.Sprintf(
			"%s in %s: %d identifiers before, %d after",
			mismatch.Filename,
			mismatch.Package,
			mismatch.ExpectedCount,
			mismatch.ActualCount,
		)
	default:
		return fmt.Sprintf(
			"%s in %s, identifier %d: expected %+v, got %+v",
			mismatch.Filename,
			mismatch.Package,
			mismatch.Index,
			mismatch.Expected,
			mismatch.Actual,
		)
	}
}
