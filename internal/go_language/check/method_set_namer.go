package check

import "go/types"

// methodSetNamer gives the method names of a type's method set, exactly the names types.NewMethodSet(set).At(index).
// Obj().Name() gives, without building the set: no types.Selection, no index path, no sorted list. indexPackage reads
// only the names, and NewMethodSet for every type name of every package of a build's closure allocated 1.15 to 1.26 GB
// a build, about 30% of a 4a build's window (#5xzb4q2).
//
// Its walk is NewMethodSet's (go/types methodset.go:72 at go1.27), kept to what a name and a collision need, and each
// rule cites the line it mirrors. A name's state at one depth is a method or a collision, two of one name, a method a
// value's set lacks, or a field. Its buffers are kept from one type to the next, so one namer reads every type name
// of a package; it is no goroutine's to share.
type methodSetNamer struct {
	// current are the types reached at the depth being walked, and next those embedded in them, one depth down, swapped
	// at each depth, so neither is allocated again.
	current []methodSetEmbedded
	next    []methodSetEmbedded

	// seen are the named types walked at a shallower depth, which a deeper one of them never is again (lookup.go:324,
	// instanceLookup, by identity, since two instances may be identical and not equal).
	seen []*types.Named

	// found is NewMethodSet's base, each name settled at the shallowest depth it was met, true for a method, false for
	// a collision, which shadows every deeper one of the name. depthMethods and depthFields are its mset and fset, the
	// methods and fields of one depth, a method true and a collision false.
	found        map[methodSetNameKey]bool
	depthMethods map[methodSetNameKey]bool
	depthFields  map[methodSetNameKey]bool

	// names are the last set's names, the answer's backing array, reused.
	names []string
}

// newMethodSetNamer is a namer with its maps made, for one goroutine to read many types with.
func newMethodSetNamer() *methodSetNamer {
	return &methodSetNamer{
		found:        map[methodSetNameKey]bool{},
		depthMethods: map[methodSetNameKey]bool{},
		depthFields:  map[methodSetNameKey]bool{},
	}
}

// methodNames are the names of set's method set, in no order, one for each method NewMethodSet holds: two packages'
// unexported methods of one spelling are the name twice. The slice is the namer's, good until its next call.
func (namer *methodSetNamer) methodNames(set types.Type) []string {
	namer.names = namer.names[:0]
	// A named pointer type has no methods, nor does its pointer's base lend it any (methodset.go:83).
	if named, isNamed := types.Unalias(set).(*types.Named); isNamed {
		if _, isPointer := named.Underlying().(*types.Pointer); isPointer {
			return namer.names
		}
	}
	base, isPointer := methodSetDereference(set)
	// A pointer to an interface has no methods (methodset.go:93).
	if isPointer && types.IsInterface(base) {
		return namer.names
	}
	clear(namer.found)
	namer.seen = namer.seen[:0]
	namer.current = append(namer.current[:0], methodSetEmbedded{embedded: base, indirect: isPointer})
	for len(namer.current) > 0 {
		namer.next = namer.next[:0]
		clear(namer.depthMethods)
		clear(namer.depthFields)
		for _, reached := range namer.current {
			if named, isNamed := types.Unalias(reached.embedded).(*types.Named); isNamed {
				// A named type seen at a shallower depth shadows itself here (methodset.go:121), so a cycle through
				// embedded pointers ends.
				if namer.wasSeen(named) {
					continue
				}
				namer.seen = append(namer.seen, named)
				// A named type's own methods (methodset.go:131), read from its origin: an instance's methods are its
				// origin's, of the same names, packages and receivers, its signatures alone substituted, which
				// Named.Method would expand and allocate for an instance, and a name never reads.
				origin := named.Origin()
				for index := range origin.NumMethods() {
					namer.addMethod(origin.Method(index), reached.indirect, reached.multiples)
				}
			}
			switch underlying := reached.embedded.Underlying().(type) {
			case *types.Struct:
				for index := range underlying.NumFields() {
					field := underlying.Field(index)
					// Every field's name is a field at this depth (methodset.go:142).
					namer.depthFields[methodSetNameKeyOf(field)] = true
					// An embedded field, T or *T, is walked one depth down, through the pointer, indirect from then on
					// (methodset.go:148).
					if field.Embedded() {
						embedded, embeddedIsPointer := methodSetDereference(field.Type())
						namer.next = append(namer.next, methodSetEmbedded{embedded: embedded,
							indirect: reached.indirect || embeddedIsPointer, multiples: reached.multiples})
					}
				}
			case *types.Interface:
				// An interface brings its type set's methods, embedded interfaces' included, as reached through a
				// pointer, so a pointer receiver never keeps one out (methodset.go:158).
				for index := range underlying.NumMethods() {
					namer.addMethod(underlying.Method(index), true, reached.multiples)
				}
			}
		}
		// A depth's methods settle the names no shallower depth settled, a method colliding with a field of its name
		// at its own depth (methodset.go:164).
		for key, isMethod := range namer.depthMethods {
			if _, settled := namer.found[key]; !settled {
				namer.found[key] = isMethod && !namer.depthFields[key]
			}
		}
		// A depth's fields settle every name still open as a collision, hiding a deeper method of it
		// (methodset.go:179).
		for key := range namer.depthFields {
			if _, settled := namer.found[key]; !settled {
				namer.found[key] = false
			}
		}
		namer.current, namer.next = namer.next, namer.current
		namer.consolidateMultiples()
	}
	// The set is the names settled as methods (methodset.go:197).
	for key, isMethod := range namer.found {
		if isMethod {
			namer.names = append(namer.names, key.name)
		}
	}
	return namer.names
}

// addMethod adds method to the depth's methods (methodset.go:228): it is a method when its name is new at the depth,
// the type it came by appears once there, and the set holds it, through a pointer or by a value receiver; otherwise
// its name collides, which a method the set lacks does too.
func (namer *methodSetNamer) addMethod(method *types.Func, indirect bool, multiples bool) {
	key := methodSetNameKeyOf(method)
	if !multiples {
		if _, known := namer.depthMethods[key]; !known && (indirect || !methodSetPointerReceiver(method)) {
			namer.depthMethods[key] = true
			return
		}
	}
	namer.depthMethods[key] = false
}

// wasSeen reports whether named, or a type identical to it, was walked at a shallower depth (lookup.go:331).
func (namer *methodSetNamer) wasSeen(named *types.Named) bool {
	for _, seen := range namer.seen {
		if seen == named || types.Identical(seen, named) {
			return true
		}
	}
	return false
}

// consolidateMultiples folds the entries of one type at the depth now current into its first, marked as appearing
// more than once (lookup.go:289), compacting namer.current in place.
func (namer *methodSetNamer) consolidateMultiples() {
	if len(namer.current) <= 1 {
		return
	}
	unique := 0
	for _, reached := range namer.current {
		earlier := -1
		for index := range unique {
			if namer.current[index].embedded == reached.embedded ||
				types.Identical(namer.current[index].embedded, reached.embedded) {
				earlier = index
				break
			}
		}
		if earlier >= 0 {
			namer.current[earlier].multiples = true
			continue
		}
		namer.current[unique] = reached
		unique++
	}
	namer.current = namer.current[:unique]
}

// methodSetDereference is the base of a pointer, an alias of one included, but never of a named pointer type, and
// whether it was one: go/types' deref (lookup.go:629). A pointer with no base is the invalid type.
func methodSetDereference(dereferenced types.Type) (types.Type, bool) {
	if pointer, isPointer := types.Unalias(dereferenced).(*types.Pointer); isPointer {
		if pointer.Elem() == nil {
			return types.Typ[types.Invalid], true
		}
		return pointer.Elem(), true
	}
	return dereferenced, false
}

// methodSetPointerReceiver reports whether method's receiver is a pointer, go/types' Func.hasPtrRecv (object.go:472)
// as its exported API can tell it: by the receiver of its signature. A method with no signature or no receiver yet,
// which a type checker that finished never leaves, reads as a value receiver, so the set holds it, the larger set.
func methodSetPointerReceiver(method *types.Func) bool {
	signature, isSignature := method.Type().(*types.Signature)
	if !isSignature || signature.Recv() == nil {
		return false
	}
	_, isPointer := methodSetDereference(signature.Recv().Type())
	return isPointer
}
