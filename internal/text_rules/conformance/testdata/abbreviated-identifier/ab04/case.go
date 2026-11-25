package ab04

func parity() int {
	userMsg := "a"
	msgCount := 1
	ctxValue := 2
	pageIdx := 3
	errMsg := "b"
	return len(userMsg) + msgCount + ctxValue + pageIdx + len(errMsg)
}
