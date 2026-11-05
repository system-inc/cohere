package flagged

import "fmt"

type settings struct {
	port  int
	größe int
}

type server struct {
	settings
	name     string
	items    []string
	options  settings
	callback func() string
}

type box[Value any] struct {
	value Value
}

var defaultServer server

// The plain shape: one name bound to the field of the same name, read once at once.
func plain(server server) string {
	name := server.name // want "`name` is a pure alias for `server.name`"
	return name
}

// A var declaration with no type is the same binding as :=.
func declared(server *server) string {
	var name = server.name // want "`name` is a pure alias for `server.name`"
	return name
}

// A deep chain of field reads, and parentheses around it.
func deep(server *server) int {
	port := (server.options.port) // want "`port` is a pure alias for `server.options.port`"
	return port
}

// A field promoted from an embedded struct is a field read like any other.
func promoted(server server) int {
	port := server.port // want "`port` is a pure alias for `server.port`"
	return port
}

// A pointer receiver's field.
func (server *server) label() string {
	name := server.name // want "`name` is a pure alias for `server.name`"
	return fmt.Sprintf("[%s]", name)
}

// A package variable's field.
func fromPackage() string {
	name := defaultServer.name // want "`name` is a pure alias for `defaultServer.name`"
	return name
}

// A generic struct's field.
func generic[Value any](container box[Value]) Value {
	value := container.value // want "`value` is a pure alias for `container.value`"
	return value
}

// A function literal judges its own locals.
func literal(server server) func() string {
	return func() string {
		name := server.name // want "`name` is a pure alias for `server.name`"
		return name
	}
}

// An if's init is the same binding, scoped to the if.
func initialized(server server) bool {
	if name := server.name; name != "" { // want "`name` is a pure alias for `server.name`"
		return true
	}
	return false
}

// A range's expression is evaluated once, before the loop's body runs.
func ranged(server server) int {
	total := 0
	items := server.items // want "`items` is a pure alias for `server.items`"
	for range items {
		total++
	}
	return total
}

// A deferred call's arguments are evaluated where the defer stands, so the reach there reads the same.
func deferred(server server) {
	name := server.name // want "`name` is a pure alias for `server.name`"
	defer fmt.Println(name)
}

// A field holding a function, called through the local: the call runs after the read.
func called(server server) string {
	callback := server.callback // want "`callback` is a pure alias for `server.callback`"
	return callback()
}

// A name in letters past ASCII.
func multiByte(server server) int {
	größe := server.größe // want "`größe` is a pure alias for `server.größe`"
	return größe
}

// A conversion, a measuring builtin, and a := of new names run no code that could change the field.
func harmlessBetween(server server) string {
	name := server.name // want "`name` is a pure alias for `server.name`"
	size := len(server.items)
	suffix := string([]byte("!"))
	if size > 0 {
		return name + suffix
	}
	return suffix
}

// A struct copied into a local and read once for one of its fields: nothing could change between.
func structCopy(server server) int {
	options := server.options // want "`options` is a pure alias for `server.options`"
	return options.port
}

// A pointer root with a return between: a nil server panics at the declaration here, and after the rewrite at the
// read or not at all, so the message says where the panic moves rather than calling the two the same.
func guarded(server *server, ready bool) string {
	name := server.name // want "pure alias .* Through a nil pointer the reach panics where `name` is read"
	if !ready {
		return ""
	}
	return name
}
