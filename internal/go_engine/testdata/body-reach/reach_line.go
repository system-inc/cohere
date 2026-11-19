package reach

// Before is placed where its file is, and uses nothing that differs.
func Before() string {
	return "before"
}

// After is placed by a //line directive in a file no build lists, and calls a function only each build's own file
// declares.
//
//line reach.rl:10
func After() string {
	return platformName()
}
