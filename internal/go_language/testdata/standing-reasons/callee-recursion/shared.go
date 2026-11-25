package audit

func target() {
	other()
	target()
}

// other returns where stop does, and stops where it stops.
func other() { stop() }
