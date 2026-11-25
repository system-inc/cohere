package load

import "crypto/sha256"

// rememberedListing is one listing a load of the run made or read kept, as cachedList answers a later ask with its key
// (Fingerprints.remembered): the listing, whether it was kept, and each module file it names that a go command of the
// run can rewrite, as it read when the listing was remembered. No reader of a listing changes it once list or the store
// has made it, so every load answered from one shares it.
type rememberedListing struct {
	listed     *listing
	kept       bool
	rewritable []rewritableModuleFile
}

// rewritableModuleFile is one module file a go command of the run can rewrite (rewritableByGo), by the digest of its
// bytes, or absent, as moduleFileDigest read it.
type rewritableModuleFile struct {
	file   string
	digest [sha256.Size]byte
	absent bool
}

// unmoved reports whether every module file remembered names reads as it did when it was remembered. One that cannot
// be read now is a doubt, and a doubt is never taken for nothing having moved.
func (remembered *rememberedListing) unmoved() bool {
	for _, then := range remembered.rewritable {
		digest, absent, readable := moduleFileDigest(then.file)
		if !readable || absent != then.absent || digest != then.digest {
			return false
		}
	}
	return true
}
