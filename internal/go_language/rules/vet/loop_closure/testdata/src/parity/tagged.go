//go:build go1.21

// A file whose build line names go1.21 is judged at go1.21 in a module and outside one alike.

package parity

func taggedAtGo121(values []int) {
	for index, value := range values {
		go func() {
			use(index, value) // want "loop variable index captured by func literal" "loop variable value captured by func literal"
		}()
	}
}
