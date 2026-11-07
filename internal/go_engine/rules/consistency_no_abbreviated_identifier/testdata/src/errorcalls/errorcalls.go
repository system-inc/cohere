// Package errorcalls holds local errors named err, each named for the call that produced it when it is written once
// by a single call and that name is free, and each keeping the advice toward a descriptive name otherwise.
//
// Its golden shows each offered rename at the declaration alone, with the uses still spelled the old way: analysistest
// applies the fix's one edit, and the engine's rename fix renames every use itself, through the types.
package errorcalls

import (
	"fmt"
	"os"
	"os/exec"
)

// A table call names its error, and the finding names the spelling and offers it.
func read(path string) []byte {
	data, err := os.ReadFile(path) // want `Identifier "err" should not be abbreviated\. Use "readError", named for the call that produced it\.`
	if err != nil {
		return nil
	}
	return data
}

// A method in the table names its error by the whole method, so exec's CombinedOutput is told apart from Output.
func run(command *exec.Cmd) []byte {
	output, err := command.CombinedOutput() // want `Identifier "err" should not be abbreviated\. Use "combinedOutputError", named for the call that produced it\.`
	if err != nil {
		return nil
	}
	return output
}

// A call of a function value names its error for the value.
func load(fetch func() error) error {
	err := fetch() // want `Identifier "err" should not be abbreviated\. Use "fetchError", named for the call that produced it\.`
	return err
}

// A declared function names its error by its own name.
func loadSettings() error { return nil }

func check() error {
	if err := loadSettings(); err != nil { // want `Identifier "err" should not be abbreviated\. Use "loadSettingsError", named for the call that produced it\.`
		return err
	}
	return nil
}

// An error written twice is two definitions a single rename can't split, so it keeps the advice.
func twice(first string, second string) error {
	_, err := os.ReadFile(first) // want `Identifier "err" should not be abbreviated\. Use a descriptive name ending in "Error", since "error" is already Go's name for something else\.`
	if err != nil {
		return err
	}
	_, err = os.ReadFile(second)
	return err
}

// The name the call gives is already declared where err is, so it keeps the advice.
func taken(path string) error {
	readError := fmt.Sprint(path)
	_, err := os.ReadFile(readError) // want `Identifier "err" should not be abbreviated\. Use a descriptive name ending in "Error", since "error" is already Go's name for something else\.`
	return err
}

// The name is declared in a scope nested in err's, which the rename would capture its use in.
func nested(path string) error {
	_, err := os.ReadFile(path) // want `Identifier "err" should not be abbreviated\. Use a descriptive name ending in "Error", since "error" is already Go's name for something else\.`
	if path != "" {
		readError := err
		return readError
	}
	return nil
}

// A constructed error names nothing.
func constructed() error {
	err := fmt.Errorf("failed") // want `Identifier "err" should not be abbreviated\. Use a descriptive name ending in "Error", since "error" is already Go's name for something else\.`
	return err
}

// An error whose address is taken may be written through the pointer.
func pointed() error {
	err := loadSettings() // want `Identifier "err" should not be abbreviated\. Use a descriptive name ending in "Error", since "error" is already Go's name for something else\.`
	pointer := &err
	return *pointer
}

// An err that is not an error keeps the advice.
func count() int {
	err := len(os.Args) // want `Identifier "err" should not be abbreviated\. Use a descriptive name ending in "Error", since "error" is already Go's name for something else\.`
	return err
}
