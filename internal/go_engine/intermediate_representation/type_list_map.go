package intermediate_representation

import (
	"go/types"

	"honnef.co/go/tools/go/types/typeutil"
)

type typeListMap struct {
	hasher  typeutil.Hasher
	buckets map[uint32][]*typeList
}

// rep returns a canonical representative of a slice of types.
func (listMap *typeListMap) rep(ts []types.Type) *typeList {
	if listMap == nil || len(ts) == 0 {
		return nil
	}

	if listMap.buckets == nil {
		listMap.buckets = make(map[uint32][]*typeList)
	}

	bucketKey := listMap.hash(ts)
	bucket := listMap.buckets[bucketKey]
	for _, list := range bucket {
		if list.identical(ts) {
			return list
		}
	}

	// not present. create a representative.
	cp := make(typeList, len(ts))
	copy(cp, ts)
	rep := &cp

	listMap.buckets[bucketKey] = append(bucket, rep)
	return rep
}

func (listMap *typeListMap) hash(ts []types.Type) uint32 {
	if listMap == nil {
		return 0
	}
	// Some smallish prime far away from typeutil.Hash.
	typeCount := len(ts)
	hash := uint32(13619) + 2*uint32(typeCount)
	for index := range typeCount {
		hash += 3 * listMap.hasher.Hash(ts[index])
	}
	return hash
}
