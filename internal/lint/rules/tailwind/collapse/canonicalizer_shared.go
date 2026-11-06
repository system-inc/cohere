package tailwind

import "sync"

// canonicalizerShared is what every feature set's Canonicalizer over one design system has in common.
type canonicalizerShared struct {
	system     *LoadedDesignSystem
	byFeatures sync.Map
	indexOnce  sync.Once
	index      *classListIndex
}

// classIndex is the index, built once per design system.
func (shared *canonicalizerShared) classIndex() *classListIndex {
	shared.indexOnce.Do(func() {
		shared.index = buildClassListIndex(shared.system)
	})
	return shared.index
}
