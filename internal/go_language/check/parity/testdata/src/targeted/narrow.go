//go:build 386

package targeted

func narrow() {
	shared() //@ diag("call to shared")
}
