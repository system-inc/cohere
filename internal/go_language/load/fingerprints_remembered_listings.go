package load

import "crypto/sha256"

// ListingsRemembered is how many listings fingerprints remembered and how many asks were answered from one, which the
// cohere-go's tests read to see a build's second listing answered from its first (#32594zm).
func (fingerprints *Fingerprints) ListingsRemembered() (int64, int64) {
	return fingerprints.listingsRemembered.Load(), fingerprints.listingsAnswered.Load()
}

// remembered is the listing remembered under key, listingKey's, whether it was kept, and whether one answers: none
// when fingerprints is nil, and none when a module file it names that a go command of the run can rewrite reads
// otherwise than it did, or cannot be read, since another build's go list may have rewritten it since, as
// listingFingerprint reads each again at every ask for the same reason.
func (fingerprints *Fingerprints) remembered(key [sha256.Size]byte) (*listing, bool, bool) {
	if fingerprints == nil {
		return nil, false, false
	}
	fingerprints.mutex.Lock()
	remembered := fingerprints.rememberedListings[key]
	fingerprints.mutex.Unlock()
	if remembered == nil || !remembered.unmoved() {
		return nil, false, false
	}
	fingerprints.listingsAnswered.Add(1)
	return remembered.listed, remembered.kept, true
}

// remember keeps listed, made or read kept in goEnvironment's workspace under key, listingKey's, for the run's later
// asks with the key, with whether it was kept, and each module file it names that a go command of the run can rewrite
// as it reads now. It remembers nothing when fingerprints is nil, when listed has an error in it, which another ask
// could answer otherwise, as keepListing keeps none (listingInputs), or when one of those module files cannot be read,
// a doubt, which a later ask could never tell from nothing having moved.
func (fingerprints *Fingerprints) remember(
	goEnvironment *GoEnvironment,
	key [sha256.Size]byte,
	listed *listing,
	kept bool,
) {
	if fingerprints == nil {
		return
	}
	inputs, rememberable := listingInputs(goEnvironment.workspace, goEnvironment.value("GOMODCACHE"),
		goEnvironment.value("GOROOT"), listed)
	if !rememberable {
		return
	}
	rewritesModules := goFlagsRewriteModules(goEnvironment.value("GOFLAGS"))
	var rewritable []rewritableModuleFile
	for _, file := range inputs.ModuleFiles {
		if !rewritableByGo(file, rewritesModules) {
			continue
		}
		digest, absent, readable := moduleFileDigest(file)
		if !readable {
			return
		}
		rewritable = append(rewritable, rewritableModuleFile{file: file, digest: digest, absent: absent})
	}
	fingerprints.mutex.Lock()
	defer fingerprints.mutex.Unlock()
	fingerprints.rememberedListings[key] = &rememberedListing{listed: listed, kept: kept, rewritable: rewritable}
	fingerprints.listingsRemembered.Add(1)
}
