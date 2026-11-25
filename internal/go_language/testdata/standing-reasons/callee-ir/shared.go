package audit

type record struct{ count int }

func target(input int, value record) {
	other()
	if value.count < 0 {
		println(input)
		return
	}
	input = 1
	input = 2
	println(input)
	var local record
	local.count = 3
	values := &[1]int{7}
	println(len(values))
	var ptr *int
	println(*ptr)
}

// other returns where stop does, and stops where it stops.
func other() { stop() }
