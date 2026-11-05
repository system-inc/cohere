package lookalike

type holder struct{ cache map[string]int }

func newCache() map[string]int { return map[string]int{} }

// An alias of the parameter: a write through it is the caller's to read.
func aliasOfParameter(source map[string]int) {
	alias := source
	alias["a"] = 1
}

// A window into a buffer the caller holds.
func windowOfBuffer(buffer []byte) {
	window := buffer[:2]
	window[0] = 1
}

// A map from a struct field, which the struct still holds.
func fromField(owner *holder) {
	cache := owner.cache
	cache["a"] = 1
}

// A map a call returns may be held by whoever made it.
func fromCall() {
	cache := newCache()
	cache["a"] = 1
}

// A var declared from a variable, not a literal.
func declaredFromVariable(source []int) {
	var alias = source
	alias[0] = 1
}

// A local function named delete may read what it is given, so it is no write.
func localDelete() {
	delete := func(values map[string]int, key string) { println(len(values), key) }
	values := map[string]int{"a": 1}
	delete(values, "a")
}

// A local function named append is no builtin either.
func localAppend() {
	append := func(values []int, value int) []int { println(len(values)); return values }
	values := make([]int, 1)
	values = append(values, 1)
	values[0] = 2
}

// A local function named make returns a map someone else may hold.
func localMake(shared map[string]int) {
	make := func() map[string]int { return shared }
	values := make()
	values["a"] = 1
}

// A pointer to a slice is no slice.
func pointerToSlice() {
	values := &[]int{1}
	(*values)[0] = 2
}

// A field write on an element is a read of the slice, then a write somewhere else.
func elementField() {
	owners := []*holder{{}}
	owners[0].cache = nil
}

// A map of slices whose element is appended to through an index reads the map first.
func groupedByKey(names []string) {
	groups := map[int][]string{}
	for _, name := range names {
		groups[len(name)] = append(groups[len(name)], name)
	}
}
