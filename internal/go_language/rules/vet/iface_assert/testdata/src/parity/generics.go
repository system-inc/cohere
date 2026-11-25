package parity

import (
	"io"

	"parity/other"
)

// Getter is generic in what it gets.
type Getter[Value any] interface {
	Get() Value
}

// SliceOf and Stringish are constraints that restrict by type alone, for embedding and for union terms.
type SliceOf[Element any] interface {
	~[]Element
}

type Stringish interface {
	~string
}

type StringExactly interface {
	string
}

// Returning is a generic alias of an interface.
type Returning[Value any] = interface {
	Returned() Value
}

// Instances of a generic interface with concrete arguments compare as any interfaces do.
func Instantiated(getter Getter[int], foreign other.Getter[int]) {
	_ = getter.(Getter[string])            // finding: Get
	_ = getter.(Getter[int])               // clean
	_ = getter.(interface{ Get() string }) // finding: Get
	_ = foreign.(Getter[string])           // finding: Get, across packages
	_ = foreign.(Getter[int])              // clean
	switch getter.(type) {
	case Getter[string], Getter[int]: // finding: Get, on Getter[string] alone
	}
}

// A type parameter among the asserted value's arguments, or the target's, silences the pass.
func FreeInAsserted[Value any](getter Getter[Value]) {
	_ = getter.(Getter[string])            // clean: Value is free
	_ = getter.(interface{ Get() string }) // clean: Value is free
}

func FreeInTarget[Value any](getter Getter[int]) {
	_ = getter.(Getter[Value]) // clean: Value is free
	switch getter.(type) {
	case Getter[Value], Getter[string]: // finding: Get, on Getter[string] alone
	}
}

// A target that is a type parameter is judged by its constraint's interface: its methods, and its terms.
func TargetMethods[Target interface{ Read() }](reader io.Reader) {
	_ = reader.(Target) // finding: Read
	switch reader.(type) {
	case Target: // finding: Read
	}
}

func TargetMethodsFree[Value any, Target interface{ Read() Value }](reader io.Reader) {
	_ = reader.(Target) // clean: Value is free in Read
}

func TargetMethodsAgree[Target io.Reader](reader io.Reader) {
	_ = reader.(Target) // clean
}

func TermsWithoutParameter[Target interface {
	~[]byte
	Read()
}](reader io.Reader) {
	_ = reader.(Target) // finding: Read
}

func TermsWithParameter[Element any, Target interface {
	~[]Element
	Read()
}](reader io.Reader) {
	_ = reader.(Target) // clean: Element is free in a term
}

func UnionWithParameter[Element any, Target interface {
	~string | ~[]Element
	Read()
}](reader io.Reader) {
	_ = reader.(Target) // clean: Element is free in a term of the union
}

func UnionWithoutParameter[Target interface {
	~string | ~[]byte
	Read()
}](reader io.Reader) {
	_ = reader.(Target) // finding: Read
}

// An intersection that leaves out the term holding the type parameter leaves nothing free.
func IntersectionDropsParameter[Element any, Target interface {
	~[]Element | int
	int
	Read()
}](reader io.Reader) {
	_ = reader.(Target) // finding: Read
}

func IntersectionKeepsParameter[Element any, Target interface {
	~[]Element | int
	~[]Element
	Read()
}](reader io.Reader) {
	_ = reader.(Target) // clean: Element is free in the term the intersection keeps
}

// A tilde term intersected with its exact type keeps the exact type, in either order.
func IntersectionOfTildeAndExact[Target interface {
	~int | string
	int
	Read()
}](reader io.Reader) {
	_ = reader.(Target) // finding: Read
}

// A term set that holds no type cannot be computed, and the pass judges the methods alone, though a term holds a
// type parameter.
func EmptyTermSet[Element any, Target interface {
	~[]Element
	string
	Read()
}](reader io.Reader) {
	_ = reader.(Target) // finding: Read
}

func EmbeddedConstraint[Element any, Target interface {
	SliceOf[Element]
	Read()
}](reader io.Reader) {
	_ = reader.(Target) // clean: Element is free in the embedded constraint's term
}

func EmbeddedConstraintConcrete[Target interface {
	SliceOf[byte]
	Read()
}](reader io.Reader) {
	_ = reader.(Target) // finding: Read
}

func UnionOfInterfaces[Element any, Target interface {
	SliceOf[Element] | Stringish
	Read()
}](reader io.Reader) {
	_ = reader.(Target) // clean: Element is free in a union term's own term
}

func UnionOfInterfacesConcrete[Target interface {
	SliceOf[byte] | Stringish
	Read()
}](reader io.Reader) {
	_ = reader.(Target) // finding: Read
}

// Two interface terms of the same type fold into the one with a tilde.
func UnionFolds[Element any, Target interface {
	Stringish | StringExactly
	Read()
}](reader io.Reader) {
	_ = reader.(Target) // finding: Read
}

func AllTerms[Target interface {
	any
	Read()
}](reader io.Reader) {
	_ = reader.(Target) // finding: Read
}

func ComparableTerms[Target interface {
	comparable
	Read()
}](reader io.Reader) {
	_ = reader.(Target) // finding: Read
}

// A type parameter reached through every kind of type a method's signature can hold silences the pass, and the same
// shape without it does not.
func Shapes[Parameter any]() {
	type Element = Parameter
	type Local struct{ held Parameter }
	type LocalReader interface{ Read() Parameter }

	var withMap interface{ Shape() map[string]Parameter }
	_ = withMap.(interface{ Shape() int }) // clean: free in a map's element
	var withMapKey interface{ Shape() map[*Parameter]int }
	_ = withMapKey.(interface{ Shape() int }) // clean: free in a map's key
	var withChannel interface{ Shape() chan Parameter }
	_ = withChannel.(interface{ Shape() int }) // clean: free in a channel's element
	var withArray interface{ Shape() [2]Parameter }
	_ = withArray.(interface{ Shape() int }) // clean: free in an array's element
	var withSlice interface{ Shape() []Parameter }
	_ = withSlice.(interface{ Shape() int }) // clean: free in a slice's element
	var withPointer interface{ Shape() *Parameter }
	_ = withPointer.(interface{ Shape() int }) // clean: free in a pointer's element
	var withStruct interface {
		Shape() struct{ first, second Parameter }
	}
	_ = withStruct.(interface{ Shape() int }) // clean: free in a struct's field
	var withFunction interface{ Shape() func(int, Parameter) }
	_ = withFunction.(interface{ Shape() int }) // clean: free in a function's parameter
	var withResults interface{ Shape() (int, Parameter) }
	_ = withResults.(interface{ Shape() int }) // clean: free in a second result
	var withParameter interface{ Shape(int, Parameter) }
	_ = withParameter.(interface{ Shape() }) // clean: free in a second parameter
	var withAlias interface{ Shape() Element }
	_ = withAlias.(interface{ Shape() int }) // clean: free in what a local alias stands for
	var withGenericAlias interface{ Shape() Returning[Parameter] }
	_ = withGenericAlias.(interface{ Shape() int }) // clean: free in a generic alias's argument
	var withLocal interface{ Shape() Local }
	_ = withLocal.(interface{ Shape() int }) // clean: free in a local type's underlying type
	var withInstance interface{ Shape() Getter[Parameter] }
	_ = withInstance.(interface{ Shape() int }) // clean: free in an instance's argument
	var localReader LocalReader
	_ = localReader.(io.Reader) // clean: free in a local interface's method

	var withConcreteMap interface{ Shape() map[string]int }
	_ = withConcreteMap.(interface{ Shape() int }) // finding: Shape
	var withConcreteStruct interface{ Shape() struct{ held int } }
	_ = withConcreteStruct.(interface{ Shape() int }) // finding: Shape
	var withConcreteFunction interface{ Shape() func(int) string }
	_ = withConcreteFunction.(interface{ Shape() int }) // finding: Shape
	var withConcreteAlias interface{ Shape() Returning[int] }
	_ = withConcreteAlias.(interface{ Shape() int }) // finding: Shape
	var withConcreteInstance interface{ Shape() Getter[int] }
	_ = withConcreteInstance.(interface{ Shape() int }) // finding: Shape
	var returning Returning[int]
	_ = returning.(interface{ Returned() string }) // finding: Returned, the asserted value's type a generic alias
	var returningFree Returning[Parameter]
	_ = returningFree.(interface{ Returned() string }) // clean: Parameter is free
}

// A generic method's body sees its receiver's type parameters as free.
type Box[Value any] struct {
	getter Getter[Value]
	reader Reader
}

func (box Box[Value]) Check() {
	_ = box.getter.(Getter[int]) // clean: Value is free
	_ = box.reader.(io.Reader)   // finding: Read, though in a generic method
}

// The search remembers what it answers across the package, and answers false for a type met inside its own
// computation. Asked first about Recursive, whose around comes before zone, it meets around's result inside
// Recursive's computation and remembers it holds nothing free, before zone finds Parameter, so a later assertion on
// that result is judged. Asked about the result first, it finds Parameter through Recursive, and stays silent.
func RememberedFirst[Parameter any]() {
	type Recursive interface {
		around() interface{ back() Recursive }
		zone() Parameter
	}
	var recursive Recursive
	_ = recursive.(interface{ zone() int }) // clean: Parameter is free
	inner := recursive.around()
	_ = inner.(interface{ back() int }) // finding: back, the remembered answer standing
}

func RememberedLast[Parameter any]() {
	type Recursive interface {
		around() interface{ back() Recursive }
		zone() Parameter
	}
	var recursive Recursive
	inner := recursive.around()
	_ = inner.(interface{ back() int })     // clean: Parameter is free through Recursive
	_ = recursive.(interface{ zone() int }) // clean: Parameter is free
}

// A type switch comes before the assertions inside it and after it in the original's preorder, so the order of
// questions is the switch's first, and the assertion below is judged by what the switch's question remembered.
func RememberedAcrossASwitch[Parameter any]() {
	type Recursive interface {
		around() interface{ back() Recursive }
		zone() Parameter
	}
	var recursive Recursive
	inner := recursive.around()
	switch recursive.(type) {
	case interface{ zone() int }: // clean: Parameter is free
	}
	_ = inner.(interface{ back() int }) // finding: back, the switch's remembered answer standing
}
