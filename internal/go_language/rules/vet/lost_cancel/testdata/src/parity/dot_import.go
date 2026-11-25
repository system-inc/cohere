package parity

// A dot import's WithCancel is no selector, so neither analyzer judges it.
import . "context"

func dotImported() {
	_, cancel := WithCancel(Background())
	if condition {
		cancel()
	}
	ctx, _ := WithTimeout(Background(), 0)
	_ = ctx
}
