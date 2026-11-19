package gate

func Target() int {
	var x struct {
		A int `default:"1" default:"2"`
	}
	return x.A
}
