package flagged

// An empty function, method or function literal is judged wherever it sits.

func nothing() {} // want "body is empty, and nothing inside it says why"

func init() {} // want "body is empty"

type recorder struct{}

func (recorder *recorder) Close() {} // want "body is empty"

func generic[Element any](element Element) {} // want "body is empty"

type box[Element any] struct{}

func (box *box[Element]) Put(element Element) {} // want "body is empty"

// A body spread over lines is as empty as one on a single line.
// want +1 "body is empty"
func spread() {
}

// A comment with no words in it says nothing.
func emptyBlockComment() { /**/ } // want "body is empty"

// want +1 "body is empty"
func slashesOnly() {
	//
}

// The only comment sits before the opening brace, outside the body.
func commentBefore()/* outside */ {} // want "body is empty"

// A doc comment tells a caller what the function is for, and the body still says nothing about why it is empty.

type sealed struct{}

// Close does nothing: a sealed value holds no file.
func (sealed *sealed) Close() {} // want "body is empty"

// node marks sealed as a node of the tree, so no type outside this package can be one.
func (sealed *sealed) node() {} // want "body is empty"

// The only comment trails the closing brace, outside the body.
// want +1 "body is empty"
func commentAfter() {
} // outside

func literals() {
	callback := func() {} // want "body is empty"
	callback()
	defer func() {}() // want "body is empty"
	run(func(int) {}) // want "body is empty"
	nested := func() func() {
		return func() {} // want "body is empty"
	}
	nested()()
}

func run(callback func(int)) {
	callback(1)
}
