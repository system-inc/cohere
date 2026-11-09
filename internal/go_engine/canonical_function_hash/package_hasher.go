package canonical_function_hash

import (
	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"

	"github.com/system-inc/cohere/internal/duplicate_functions"
)

// packageHasher holds the digests of a package's closures, so a parent hashes a closure it makes by the closure's own
// body, and a closure is hashed once however many functions make it.
type packageHasher struct {
	closureDigests map[*intermediate_representation.Function]duplicate_functions.Digest
	inProgress     map[*intermediate_representation.Function]bool
}

func newPackageHasher() *packageHasher {
	return &packageHasher{
		closureDigests: map[*intermediate_representation.Function]duplicate_functions.Digest{},
		inProgress:     map[*intermediate_representation.Function]bool{},
	}
}

// closureDigest is a closure's digest, by its body, or by its name when it is being hashed already, as a closure that
// reaches itself does.
func (hasher *packageHasher) closureDigest(function *intermediate_representation.Function) duplicate_functions.Digest {
	if known, isKnown := hasher.closureDigests[function]; isKnown {
		return known
	}
	if hasher.inProgress[function] {
		return duplicate_functions.NewDigestBuilder("recursive-closure").Text(function.RelativeString(nil)).Sum()
	}
	hasher.inProgress[function] = true
	result := newFunctionHasher(hasher, function).functionDigest()
	delete(hasher.inProgress, function)
	hasher.closureDigests[function] = result
	return result
}

// digest is any function's digest: a closure through closureDigest, so its parent and it agree, any other by its body.
func (hasher *packageHasher) digest(function *intermediate_representation.Function) duplicate_functions.Digest {
	if function.Parent() != nil {
		return hasher.closureDigest(function)
	}
	return newFunctionHasher(hasher, function).functionDigest()
}
