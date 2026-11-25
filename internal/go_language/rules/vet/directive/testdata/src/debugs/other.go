package debugs

// unsettled holds another misplaced //go:debug line, and calls back into the first file.
func unsettled() string {
	//go:debug http2client=0
	if settled() == "" {
		return ""
	}
	return "unsettled"
}

// quiet holds no directive, so it reports nothing.
func quiet() string {
	return Summary()
}
