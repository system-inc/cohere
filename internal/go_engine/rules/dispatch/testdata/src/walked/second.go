package walked

func nested() func() bool {
	return func() bool {
		if true {
			return true
		} else if false {
			return false
		}
		return false
	}
}
