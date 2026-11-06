package allowed

import "strconv"

type settings struct {
	name  string
	count int
}

func (receiver *settings) increment() { receiver.count++ }

// Writes through a parameter change what it points at, not the parameter, and are another rule's concern.
func writtenThrough(options *settings, values []int, lookup map[string]int, pointer *int) {
	options.name = "changed"
	options.count++
	values[0] = 1
	values[1] += 2
	lookup["key"] = 3
	*pointer = 4
	*pointer++
}

func (receiver *settings) renamed(name string) {
	receiver.name = name
	*receiver = settings{name: name}
}

// Named results exist to be assigned, so every assignment to one, and its address, is left alone.
func namedResults(text string) (parsed int, parseError error) {
	parsed, parseError = strconv.Atoi(text)
	parsed += 1
	parsed++
	pointer := &parsed
	*pointer = 2
	return parsed, parseError
}

// A := in an inner scope declares a new variable of the same name, and the parameter is untouched.
func shadowed(value int, values []int) int {
	if value > 0 {
		value := value * 2
		value++
		_ = value
	}
	for _, value := range values {
		value += 1
		_ = value
	}
	for value := 0; value < 3; value++ {
		_ = value
	}
	return value
}

func typeSwitched(value any) string {
	switch value := value.(type) {
	case int:
		return strconv.Itoa(value)
	}
	return ""
}

// A new variable named for what the value has become is the cure the message names.
func copiedFirst(value int, options settings) (int, *settings) {
	doubled := value * 2
	doubled++
	copied := options
	copied.count = doubled
	return doubled, &copied
}

func blank(_ int, _ string) int {
	return 0
}

func unnamed(int, string) int {
	return 0
}

// A field or element's address points into the value, a write through it rather than a new value for the name.
func fieldAddress(options settings, values [3]int) (*string, *int) {
	return &options.name, &values[0]
}

// A method with a pointer receiver called on a value parameter takes its address implicitly, with nothing written.
func implicitAddress(options settings) settings {
	options.increment()
	return options
}
