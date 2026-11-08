// Package parity holds every shape the two patterns accept or refuse: parentheses at each level, the operators the
// patterns name beside the ones they do not, the names cgo generates and names that only resemble them, and the star
// of a type, which is no dereference.
package parity

type record struct {
	field int
}

type pointer *record

func use(_ any) {}

func addressOfDereference(value *record, values []*record, table map[string]*record, chosen **record) {
	use(&*value)
	use(&(*value))
	use((&*value))
	use(&*(value))
	use(&(*value))
	use(&*values[0])
	use(&*table["key"])
	use(&**chosen)
	use(&*&*value)
	use(&*pointer(value))
	use(&(*value).field)
	use(&value.field)
	use(*value)
	use(-*&value.field)
}

func dereferenceOfAddress(value record, values []record, chosen **record) {
	use(*&value)
	use(*(&value))
	use((*&value))
	use(*&(value))
	use(*&values[0])
	use(*&record{})
	use(**&chosen)
	use(*&*&value)
}

func cgoNames() {
	_Cvar_something := &record{}
	_Cfunc_something := &record{}
	_Ctype_something := &record{}
	_Cvar_ := &record{}
	_Cvarnot := &record{}
	use(&*_Cvar_something)
	use(&*(_Cvar_something))
	use(&(*_Cvar_something))
	use(&*_Cfunc_something)
	use(&*_Ctype_something)
	use(&*_Cvar_)
	use(&*_Cvarnot)
	use(*&_Cvar_something)
}

func notCopies(value *record, number int, channel chan int) {
	var typed *record
	use(typed)
	use([]*record{value})
	use(-number)
	use(^number)
	use(<-channel)
	use(!(number > 0))
	use(&number)
	use(*value)
}

func generic[T any](value *T, plain T) {
	use(&*value)
	use(*&plain)
}
