package lookalike

// A local named unsafe holds a field named Pointer, and calling the field converts nothing.
func shadowedUnsafe(value *int) uintptr {
	unsafe := struct {
		Pointer func(*int) uintptr
		String  func(*byte, int) string
	}{
		Pointer: func(*int) uintptr { return 0 },
		String:  func(*byte, int) string { return "" },
	}
	_ = unsafe.String(nil, 0)
	return unsafe.Pointer(value)
}
