package shadow

// append at package scope is not the builtin, so neither reports the guard around a call of it.
func append(s []string, values ...string) []string { return s }

func guard(m map[string][]string, k string) {
	if _, ok := m[k]; ok {
		m[k] = append(m[k], "a")
	} else {
		m[k] = []string{"a"}
	}
}
