package tailwind

import "math/big"

// RunVariantOrder is the dense index assigned to each variant for one run.
//
// Built by BuildVariantOrder over exactly the variants a run parsed, never package-level, because
// the index is a rank within that population rather than a fact about the variant. See the file
// comment.
type RunVariantOrder struct {
	// entries holds each distinct variant and its index, in sorted order.
	entries []variantOrderEntry
}

// Length reports how many distinct variants the order holds.
func (order *RunVariantOrder) Length() int {
	return len(order.entries)
}

// IndexOf returns the dense index assigned to a variant, and whether it was assigned one.
//
// Looked up by value equality rather than by identity, for the reason variantsAreEqual exists.
func (order *RunVariantOrder) IndexOf(variant ParsedVariant) (int, bool) {
	for _, entry := range order.entries {
		if variantsAreEqual(entry.variant, variant) {
			return entry.index, true
		}
	}
	return 0, false
}

// Variants returns each variant and its index, in sorted order.
func (order *RunVariantOrder) Variants() []ParsedVariant {
	variants := make([]ParsedVariant, 0, len(order.entries))
	for _, entry := range order.entries {
		variants = append(variants, entry.variant)
	}
	return variants
}

// VariantBitmask builds the mask `compile.ts` sorts on, for one candidate's variants.
//
// Upstream is three lines:
//
//	let variantOrder = 0n
//	for (let variant of candidate.variants) variantOrder |= 1n << BigInt(variantOrderMap.get(variant)!)
//
// A `big.Int` rather than a `uint64` because the shift is by the variant's dense index and a class
// list using more than 64 distinct variants is ordinary in a large repository. The corpus this is
// tested against reaches 143 distinct variants in one repository's lists, so a 64-bit mask would
// have silently wrapped rather than overflowed loudly, producing a mask that compares wrong against
// exactly the variants a small test never uses.
//
// Reports false when any variant has no index, which happens only when the order was built from a
// different population than the candidate came from. Returning a partial mask instead would be a
// mask missing a bit, and a missing high bit reorders the class against every other.
func (order *RunVariantOrder) VariantBitmask(variants []ParsedVariant) (*big.Int, bool) {
	mask := new(big.Int)
	for _, variant := range variants {
		index, hasIndex := order.IndexOf(variant)
		if !hasIndex {
			return nil, false
		}
		mask.SetBit(mask, index, 1)
	}
	return mask, true
}
