package store

import (
	"fmt"
	"reflect"
)

// pageHTML is named by its type's printed name, which %T spells with the package, so it keeps its name.
type pageHTML struct{}

// rowCSV is named by reflect's name for its type, so it keeps its name.
type rowCSV struct{}

// IsPage reads a value's type by the name %T prints.
func IsPage(value pageHTML) bool {
	return fmt.Sprintf("%T", value) == "store.pageHTML"
}

// IsRow reads a value's type by the name reflect gives it.
func IsRow(value rowCSV) bool {
	return reflect.TypeOf(value).Name() == "rowCSV"
}

// Kinds reads both types.
func Kinds() bool {
	return IsPage(pageHTML{}) && IsRow(rowCSV{})
}
