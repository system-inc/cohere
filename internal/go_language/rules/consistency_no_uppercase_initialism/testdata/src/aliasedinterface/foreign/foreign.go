// Package foreign is another package's code, which aliasedinterface imports and the test never analyzes. HomeURL is
// declared by no named type here or anywhere: only an alias names the interface that carries it.
package foreign

// Located is an alias of an interface written in place, so it declares no named interface of its own.
type Located = interface {
	HomeURL() string
	Region() string
}
