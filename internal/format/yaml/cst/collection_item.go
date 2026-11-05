package cst

// CollectionItem is an item of a block map, block sequence or flow collection, upstream's CollectionItem
// and the item types of BlockMap and BlockSequence.
type CollectionItem struct {
	Start []*Token
	// Key is nil when absent or null; HasKey tells which.
	Key *Token
	// Sep is nil when absent. Present and empty is truthy upstream.
	Sep   []*Token
	Value *Token
	// ExplicitKey is `explicitKey: true`; upstream never sets it false.
	ExplicitKey bool

	// keyPresent is `'key' in item`: true for a key that is a token or null.
	keyPresent bool
}

// HasKey is `'key' in item`: whether the item has a key field, perhaps null.
func (item *CollectionItem) HasKey() bool { return item.keyPresent }

// setKey is `item.key = key` (or Object.assign with a key), where a nil key is null.
func (item *CollectionItem) setKey(key *Token) {
	item.Key = key
	item.keyPresent = true
}

// deleteKey is `delete item.key`.
func (item *CollectionItem) deleteKey() {
	item.Key = nil
	item.keyPresent = false
}
