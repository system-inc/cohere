// Package parity holds calls whose results are assigned and then closed in a defer, early and late, and every shape
// the check reads an assignment, a result type or a deferred call in, where the port and staticcheck's SA5001 still
// agree finding for finding. The differential test runs both over each and compares.
package parity

import (
	"io"
	"os"
)

type closer struct{}

func (closer) Close() error { return nil }

type holder struct {
	inner struct{ file *os.File }
}

type error2 interface{ Error() string }

type localError = error

func open() (*os.File, error)                      { return nil, nil }
func openThree() (*os.File, int, error)            { return nil, 0, nil }
func openOther() (*os.File, error2)                { return nil, nil }
func openAliased() (*os.File, localError)          { return nil, nil }
func openOne() *os.File                            { return nil }
func openHolder() (holder, error)                  { return holder{}, nil }
func openCloser() (closer, error)                  { return closer{}, nil }
func openGeneric[T any](value T) (*os.File, error) { return nil, nil }

var openValue = func() (io.ReadCloser, error) { return nil, nil }

// Found by both: the plain case.
func plain() {
	file, err := open()
	defer file.Close()
	_ = err
}

// Found by both: assigned, not declared.
func assigned() {
	var file *os.File
	var err error
	file, err = open()
	defer file.Close()
	_ = err
}

// Found by both: three results, the error last.
func threeResults() {
	file, count, err := openThree()
	defer file.Close()
	_, _ = count, err
}

// Found by both: a selector chain rooted at the first variable.
func chained() {
	value, err := openHolder()
	defer value.inner.file.Close()
	_ = err
}

// Found by both: a Close of a type of its own.
func ownCloser() {
	value, err := openCloser()
	defer value.Close()
	_ = err
}

// Found by both: a call through a function value.
func functionValue() {
	reader, err := openValue()
	defer reader.Close()
	_ = err
}

// Found by both: a generic function, instantiated by inference.
func generic() {
	file, err := openGeneric(1)
	defer file.Close()
	_ = err
}

// Found by both: a generic function, instantiated explicitly.
func genericExplicit() {
	file, err := openGeneric[string]("x")
	defer file.Close()
	_ = err
}

// Found by both: a package's function, printed with its package's name.
func packageFunction() {
	file, err := os.Create("path")
	defer file.Close()
	_ = err
}

// Found by both: inside a function literal.
func insideLiteral() func() {
	return func() {
		file, err := open()
		defer file.Close()
		_ = err
	}
}

// Found by neither: the error checked before the defer.
func checkedFirst() {
	file, err := open()
	if err != nil {
		return
	}
	defer file.Close()
}

// Found by neither: the error thrown away.
func blankError() {
	file, _ := open()
	defer file.Close()
}

// Found by neither: one result.
func oneResult() {
	file := openOne()
	defer file.Close()
}

// Found by neither: a last result of another interface type, which does not print as error.
func otherErrorType() {
	file, err := openOther()
	defer file.Close()
	_ = err
}

// Found by neither: a last result of an alias of error, which prints as the alias.
func aliasedError() {
	file, err := openAliased()
	defer file.Close()
	_ = err
}

// Found by neither: the call in parentheses, which are not looked through.
func parenthesizedCall() {
	file, err := (open())
	defer file.Close()
	_ = err
}

// Found by neither: the first variable is a field, not a name.
func fieldFirst() {
	var value struct{ file *os.File }
	var err error
	value.file, err = open()
	defer value.file.Close()
	_ = err
}

// Found by neither: the first variable is the blank identifier.
func blankFirst(other *os.File) {
	_, err := open()
	defer other.Close()
	_ = err
}

// Found by neither: another variable is closed.
func otherVariable(other *os.File) {
	file, err := open()
	defer other.Close()
	_, _ = file, err
}

// Found by neither: a method other than Close is deferred.
func otherMethod() {
	file, err := open()
	defer file.Sync()
	_ = err
}

// Found by neither: the deferred function is no selector.
func deferredLiteral() {
	file, err := open()
	defer func() { file.Close() }()
	_ = err
}

// Found by neither: the selector roots at parentheses.
func parenthesizedRoot() {
	file, err := open()
	defer (file).Close()
	_ = err
}

// Found by neither: a statement between the assignment and the defer.
func statementBetween() {
	file, err := open()
	_ = err
	defer file.Close()
}

// Found by neither: a declaration with var, not an assignment.
func varDeclaration() {
	var file, err = open()
	defer file.Close()
	_ = err
}

// Found by neither: the assignment is the block's last statement.
func lastStatement() {
	file, err := open()
	_, _ = file, err
}
