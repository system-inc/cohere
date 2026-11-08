//go:build go1.19

// A build line older than go1.21 is raised to go1.21 by the type checker, so this file is judged too.

package parity

func taggedAtGo119(values []int) {
	for _, value := range values {
		defer func() {
			use(value) // want "loop variable value captured by func literal"
		}()
	}
}
