package refused

// TestCount takes a count, which go test never passes, so go test refuses it.
func TestCount(count int) {
	_ = Count() + count
}
