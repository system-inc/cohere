//go:build go1.22

// A file at go1.22 gives each iteration its own variables, so nothing here is reported.

package parity

func atGo122(values []int) {
	for index, value := range values {
		go func() {
			use(index, value)
		}()
	}
	for index := 0; index < 10; index++ {
		defer func() {
			use(index)
		}()
	}
}
