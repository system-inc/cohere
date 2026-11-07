// Package dispatch calls a handler's method by a name it is given, so the handler's methods keep their names though
// no Go literal spells them.
package dispatch

import "reflect"

// handler's methods are called by name.
type handler struct{}

// RunHTTP is called when the name given is its own.
func (handler) RunHTTP() string {
	return "ran"
}

// Call calls the method of the handler that name names.
func Call(name string) {
	method := reflect.ValueOf(handler{}).MethodByName(name)
	if method.IsValid() {
		method.Call(nil)
	}
}
