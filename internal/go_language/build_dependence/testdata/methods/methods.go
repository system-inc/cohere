package methods

// Plain is a shared type that gains a method in linux's file only.
type Plain struct {
	Value int
}

// Describer is what Plain implements on linux.
type Describer interface {
	Describe() string
}

// Boxed converts a Plain to an interface, which carries its method set: dependent.
func Boxed() any { return Plain{} }

// IsDescriber asks whether a value implements Describer, which reads nothing of Plain: independent.
func IsDescriber(value any) bool {
	_, isDescriber := value.(Describer)
	return isDescriber
}

// Wrapper embeds Plain, so it promotes Plain's methods.
type Wrapper struct {
	Plain
}

// Wrapped hands on a Wrapper by pointer, whose method set Plain's moves: dependent.
func Wrapped(wrapper *Wrapper) *Wrapper { return wrapper }

// Count is a shared type no platform's file touches.
type Count int

// Counted uses Count: independent.
func Counted() Count { return 1 }

// Titled embeds Describer, an interface no build changes, so its method set is Describer's and its own Title.
type Titled interface {
	Describer
	Title() string
}

// Carrier embeds the Describer interface, whose Describe it promotes whatever it holds.
type Carrier struct {
	Describer
}

// Described takes Describe as a method value promoted through Carrier's embedded interface, which reads nothing of
// Plain: independent.
func Described(carrier Carrier) func() string { return carrier.Describe }

// Announcer embeds Speaking, which embeds Describer on linux alone, so Announcer's method set moves with it.
type Announcer interface {
	Speaking
	Volume() int
}

// Announces takes Volume as a method value of an Announcer, whose method set moves on linux: dependent there.
func Announces(announcer Announcer) func() int { return announcer.Volume }

// Pair is generic over its element.
type Pair[Element any] struct {
	First  Element
	Second Element
}

// Swapped is a method of every instance of Pair.
func (pair Pair[Element]) Swapped() Pair[Element] {
	return Pair[Element]{First: pair.Second, Second: pair.First}
}

// PairOf is generic, its element inferred at each call.
func PairOf[Element any](first Element, second Element) Pair[Element] {
	return Pair[Element]{First: first, Second: second}
}

// Plains instantiates Pair with Plain, whose method set moves on linux: dependent there.
func Plains() Pair[Plain] { return PairOf(Plain{}, Plain{}) }

// SwapsCounts takes Swapped as a method value of Pair[Count], whose element gains Tagged under the tag: dependent
// there.
func SwapsCounts(pair Pair[Count]) func() Pair[Count] { return pair.Swapped }

// SwapsWholes takes the same method value of Pair[int], which no build changes: independent.
func SwapsWholes(pair Pair[int]) func() Pair[int] { return pair.Swapped }

// TitleEach is generic over an element Titled constrains, whose embedded Describer no build changes: independent.
func TitleEach[Element Titled](elements []Element) []string {
	titles := make([]string, 0, len(elements))
	for _, element := range elements {
		titles = append(titles, element.Title()+element.Describe())
	}
	return titles
}
