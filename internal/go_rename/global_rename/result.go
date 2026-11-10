package global_rename

import (
	"slices"

	"github.com/system-inc/cohere/internal/go_rename/rename"
	"github.com/system-inc/cohere/internal/go_rename/workspace"
)

// Result is the verified plan and everything a reader needs to trust it.
type Result struct {
	Accepted   []Rename
	Rejections []Rejection
	// Edits rename identifiers; TagEdits add the tags -tag-encoded-fields asked for; CommentEdits respell the
	// old name in the renamed declaration's own doc and line comments; TwinEdits rename a platform twin's
	// declarations, and respell their doc comments, in a build-constrained file the load does not check.
	Edits        []rename.Edit
	TagEdits     []rename.Edit
	CommentEdits []rename.Edit
	TwinEdits    []rename.Edit
	// EncodedFields lists every proposed field the encoding guard found reachable.
	EncodedFields []EncodedField
	// The model checks: any entry means the engine's model of Go disagreed with go/types somewhere, and every
	// rename the disagreement touched was refused.
	Unresolved                  []string
	SelectionModelMismatches    []string
	SatisfactionModelMismatches []string
	// OpaqueEncoderArguments are values handed to an encoder whose concrete types the guard could not follow.
	OpaqueEncoderArguments []string
	Notes                  []string
	// platforms are the platforms the plan read files under, which Apply verifies the rewrite under too.
	platforms []workspace.Platform
	// platformKeys are, by platform, the candidates that reach a package holding a file only that platform builds.
	platformKeys map[workspace.Platform]map[Key]bool
	// platformDifferences are, by platform, the fingerprint of each platform's load the plan read, taken before any
	// rename and kept as its difference from hostFingerprint, which the platform's proof reads in place of loading the
	// platform again (ProvePlatform).
	platformDifferences map[workspace.Platform]platformDifference
	// planned is the workspace the plan read, set when it read a platform: the differences are from its fingerprint,
	// so only an expectation of it carries them (Expect).
	planned *workspace.Workspace
	// hostFingerprint is planned's fingerprint before any rename, taken for the differences and kept for Expect over
	// planned, which takes it rather than fingerprint the tree again, and releases it here.
	hostFingerprint *unrenamedFingerprint
}

// Platforms are the platforms the plan read files under, in order, each of which the rewrite must be proven under
// as well as the host (ProvePlatform).
func (result *Result) Platforms() []workspace.Platform {
	return slices.Clone(result.platforms)
}

// RenamesUnder are the accepted renames that reach a package holding a file only platform builds, the ones a proof
// that fails under it names, in the order of Accepted.
func (result *Result) RenamesUnder(platform workspace.Platform) []Rename {
	var under []Rename
	for _, accepted := range result.Accepted {
		if result.platformKeys[platform][accepted.Key] {
			under = append(under, accepted)
		}
	}
	return under
}
