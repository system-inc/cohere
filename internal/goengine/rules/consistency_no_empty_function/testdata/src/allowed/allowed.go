package allowed

// A body that says why it is empty is left alone, in either form of comment, wherever the function sits.

type recorder struct{}

func (recorder *recorder) Close() {
	// Nothing to release: a recorder holds no file or connection.
}

func inline() { /* Nothing to do: importing this package is the point. */ }

type box[Element any] struct{}

func (box *box[Element]) Put(element Element) {
	// Ignored: a box only stands in for a store in its callers' tests.
}

func literals() {
	callback := func() {
		// Ignored: the event carries nothing this caller needs.
	}
	callback()
	defer func() { /* Recovering is the caller's job. */ }()
}

// A comment in any script says why as well as one in English.
func multiByte() {
	// 何もしない。呼び出し側が処理する。
}

func holdsStatement() {
	inline()
}
