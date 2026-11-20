package gate

// Foo is the type both builds declare, whose method Bar one build alone declares.
type Foo struct{}

// Shared is what both builds declare.
func Shared() int { return 1 }
