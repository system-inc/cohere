package intermediate_representation

import "go/types"

// A type for representing a canonized list of types.
type typeList []types.Type

func (list *typeList) identical(ts []types.Type) bool {
	if list == nil {
		return len(ts) == 0
	}
	length := len(*list)
	if len(ts) != length {
		return false
	}
	for index, left := range *list {
		right := ts[index]
		if !types.Identical(left, right) {
			return false
		}
	}
	return true
}
