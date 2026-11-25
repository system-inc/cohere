package pkg

// inner is used only through outer, which embeds it, and its field only through promotion.
type inner struct { //@ used("inner", true)
	value int //@ used("value", true)
}

// unusedMethod belongs to a type used only through an embedded field, and nothing calls it.
func (inner) unusedMethod() {} //@ used("unusedMethod", false)

type outer struct { //@ used("outer", true)
	inner //@ used("inner", true)
}

func Promoted() int { //@ used("Promoted", true)
	var embedding outer //@ used("embedding", true)
	return embedding.value
}

// recursive is used only by itself, a use whose user and used are one node.
func recursive() { //@ used("recursive", false)
	recursive()
}

// unusedCaller and unusedCallee are used only by each other, the user changing between the two bodies.
func unusedCaller() { //@ used("unusedCaller", false)
	unusedCallee()
}

func unusedCallee() { //@ used("unusedCallee", false)
	unusedCaller()
}

// Generic reads its receiver's type parameter, which go/types records in Defs and in Uses with one object.
type Generic[Element any] struct { //@ used("Generic", true), used("Element", true)
	items []Element //@ used("items", false)
}

func (generic Generic[Element]) First() { //@ used("First", true), used("generic", true), used("Element", true)
	_ = func(parameter Element) {} //@ used("parameter", true)
}

// selfEmbedding embeds itself after an exported field. Each field's search for exported fields starts having seen the
// struct's own type, so the embedded field, the second, is not used by the exported field it promotes back.
type selfEmbedding struct { //@ used("selfEmbedding", true)
	Exported       int //@ used("Exported", true)
	*selfEmbedding     //@ used("selfEmbedding", false)
}

var SelfEmbedded selfEmbedding //@ used("SelfEmbedded", true)
