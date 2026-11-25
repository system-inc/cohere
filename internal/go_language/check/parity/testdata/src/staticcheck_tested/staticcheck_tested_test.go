package staticcheck_tested

func testedCalls() {
	shared() //@ diag("call to shared")
}
