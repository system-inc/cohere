package allowed

import "strings"

type configuration struct {
	name string
	port int
}

type service struct {
	configuration configuration
	name          string
}

var defaultService service

// At package scope a field read is a declaration of the package's own, not a local renaming one.
var name = defaultService.name

// The reach written where the value is read, which is what the rule asks for.
func reached(service service) string {
	return service.name + ":" + service.configuration.name
}

// A local of another name says what the value is for here, so it is no pure rename.
func renamed(service *service) configuration {
	current := service.configuration
	return current
}

// A local the function makes from the field holds a value the field does not.
func computed(service service) string {
	name := strings.ToUpper(service.name)
	return name
}

// A local declared from a literal or a parameter is no field read at all.
func fromParameter(port int) int {
	value := port
	return value
}
