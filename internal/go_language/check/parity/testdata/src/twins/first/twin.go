// Package first holds a call to here on the line where second, in a file of the same name, calls there.
package first

func here() {}

func calls() {
	here()
}
