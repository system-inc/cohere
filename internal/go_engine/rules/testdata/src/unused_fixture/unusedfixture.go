package unused_fixture

// Exported counts as used because another package could call it, and its call is what keeps called in use.
func Exported() int {
	return called()
}

// called is unexported and used, so U1000 says nothing about it.
func called() int {
	return 1
}

func neverCalled() {} // want `func neverCalled is unused`
