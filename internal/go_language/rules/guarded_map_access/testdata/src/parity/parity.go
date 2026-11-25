package parity

type names []string

const one = 1

func key() string { return "k" }

func appends(m map[string][]string, k string, s []string) {
	// The plain shape, and the near misses of each part of the header.
	if _, ok := m[k]; ok {
		m[k] = append(m[k], "a", "b")
	} else {
		m[k] = []string{"a", "b"}
	}
	if _, found := m[k]; found {
		m[k] = append(m[k], "a")
	} else {
		m[k] = []string{"a"}
	}
	if v, ok := m[k]; ok {
		m[k] = append(v, "a")
	} else {
		m[k] = []string{"a"}
	}
	var ok bool
	if _, ok = m[k]; ok {
		m[k] = append(m[k], "a")
	} else {
		m[k] = []string{"a"}
	}
	if _, ok := m[k]; !ok {
		m[k] = append(m[k], "a")
	} else {
		m[k] = []string{"a"}
	}
	if _, ok := m[k]; ok && len(s) > 0 {
		m[k] = append(m[k], "a")
	} else {
		m[k] = []string{"a"}
	}
	// Parentheses are looked through in the header's index, the body and the else; gofmt strips them from the
	// condition, so testdata/unformatted holds that shape.
	if _, ok := (m[k]); ok {
		m[k] = (append)((m)[k], ("a"))
	} else {
		(m[k]) = ([]string{"a"})
	}
	// The literal's type is any; only its elements are recalled.
	if _, ok := m[k]; ok {
		m[k] = append(m[k], "a")
	} else {
		m[k] = names{"a"}
	}
	// Values that differ, and a keyed element.
	if _, ok := m[k]; ok {
		m[k] = append(m[k], "a")
	} else {
		m[k] = []string{"b"}
	}
	if _, ok := m[k]; ok {
		m[k] = append(m[k], "a")
	} else {
		m[k] = []string{0: "a"}
	}
	// The index recalled by syntax: a different key, a different spelling of the same key, a different map.
	if _, ok := m["k"]; ok {
		m["j"] = append(m["k"], "a")
	} else {
		m["k"] = []string{"a"}
	}
	if _, ok := m["k"]; ok {
		m["k"] = append(m[`k`], "a")
	} else {
		m["k"] = []string{"a"}
	}
	other := map[string][]string{}
	if _, ok := m[k]; ok {
		m[k] = append(other[k], "a")
	} else {
		m[k] = []string{"a"}
	}
	// The append's first argument is not the index.
	if _, ok := m[k]; ok {
		m[k] = append(s, "a")
	} else {
		m[k] = []string{"a"}
	}
	// More than one statement in a branch, an absent, an empty and an else-if branch.
	if _, ok := m[k]; ok {
		m[k] = append(m[k], "a")
		m[k] = append(m[k], "b")
	} else {
		m[k] = []string{"a"}
	}
	if _, ok := m[k]; ok {
		m[k] = append(m[k], "a")
	}
	if _, ok := m[k]; ok {
		m[k] = append(m[k], "a")
	} else {
	}
	if _, ok := m[k]; ok {
		m[k] = append(m[k], "a")
	} else if len(s) > 0 {
		m[k] = []string{"a"}
	}
	// A guard nested in an else block and in a case, and an outer guard of another shape around one.
	if len(s) > 0 {
	} else {
		if _, ok := m[k]; ok {
			m[k] = append(m[k], "a")
		} else {
			m[k] = []string{"a"}
		}
	}
	switch len(s) {
	case 0:
		if _, ok := m[k]; ok {
			m[k] = append(m[k], "a")
		} else {
			m[k] = []string{"a"}
		}
	}
	// A label on the if stays, and the fix replaces the if after it.
labeled:
	if _, ok := m[k]; ok {
		m[k] = append(m[k], "a")
	} else {
		m[k] = []string{"a"}
	}
	if len(s) > 5 {
		goto labeled
	}
	// Side effects in the index: a call, even len, a receive; arithmetic is none.
	if _, ok := m[key()]; ok {
		m[key()] = append(m[key()], "a")
	} else {
		m[key()] = []string{"a"}
	}
	if _, ok := m[s[len(s)-1]]; ok {
		m[s[len(s)-1]] = append(m[s[len(s)-1]], "a")
	} else {
		m[s[len(s)-1]] = []string{"a"}
	}
	channel := make(chan string)
	if _, ok := m[<-channel]; ok {
		m[<-channel] = append(m[<-channel], "a")
	} else {
		m[<-channel] = []string{"a"}
	}
	if _, ok := m[k+"x"]; ok {
		m[k+"x"] = append(m[k+"x"], "a")
	} else {
		m[k+"x"] = []string{"a"}
	}
	// A map reached through a field and an index.
	holder := struct{ m map[string][]string }{m}
	if _, ok := holder.m[k]; ok {
		holder.m[k] = append(holder.m[k], "a")
	} else {
		holder.m[k] = []string{"a"}
	}
	maps := []map[string][]string{m}
	if _, ok := maps[0][k]; ok {
		maps[0][k] = append(maps[0][k], "a")
	} else {
		maps[0][k] = []string{"a"}
	}
	// Formatted across lines, with comments, which the fix's printing drops.
	if _, ok := m[k]; ok {
		m[k] = append(m[k], // first
			"a",
			"b") // last
	} else {
		m[k] = []string{"a", "b"}
	}
}

func shadowedAppend(m map[string][]string, k string) {
	append := func(s []string, values ...string) []string { return s }
	if _, ok := m[k]; ok {
		m[k] = append(m[k], "a")
	} else {
		m[k] = []string{"a"}
	}
}

func adds(m map[string]int, f map[string]float64, s map[string]string, k string, v int) {
	if _, ok := m[k]; ok {
		m[k] += v
	} else {
		m[k] = v
	}
	if _, ok := m[k]; ok {
		m[k] += (v)
	} else {
		m[k] = v
	}
	if _, ok := m[k]; ok {
		m[k] += v
	} else {
		m[k] = v + 0
	}
	if _, ok := m[k]; ok {
		m[k] -= v
	} else {
		m[k] = v
	}
	if _, ok := m[k]; ok {
		m[k] *= v
	} else {
		m[k] = v
	}
	if _, ok := m[k]; ok {
		m[k] = m[k] + v
	} else {
		m[k] = v
	}
	if _, ok := f[k]; ok {
		f[k] += 2.5
	} else {
		f[k] = 2.5
	}
	if _, ok := s[k]; ok {
		s[k] += "x"
	} else {
		s[k] = "x"
	}
	if _, ok := m[k]; ok {
		m[k] += 4
	} else {
		m["k"] = 4
	}
}

func increments(m map[string]int, f map[string]float64, u map[string]uint8, c map[string]complex128, k string) {
	if _, ok := m[k]; ok {
		m[k]++
	} else {
		m[k] = 1
	}
	if _, ok := m[k]; ok {
		(m[k])++
	} else {
		m[k] = (1)
	}
	if _, ok := m[k]; ok {
		m[k]++
	} else {
		m[k] = +1
	}
	if _, ok := m[k]; ok {
		m[k]++
	} else {
		m[k] = -(-1)
	}
	if _, ok := m[k]; ok {
		m[k]++
	} else {
		m[k] = 0x1
	}
	if _, ok := m[k]; ok {
		m[k]++
	} else {
		m[k] = 0b1
	}
	if _, ok := m[k]; ok {
		m[k]++
	} else {
		m[k] = one
	}
	if _, ok := m[k]; ok {
		m[k]++
	} else {
		m[k] = int(1)
	}
	if _, ok := m[k]; ok {
		m[k]++
	} else {
		m[k] = 2 - 1
	}
	if _, ok := m[k]; ok {
		m[k]++
	} else {
		m[k] = 2
	}
	if _, ok := m[k]; ok {
		m[k]--
	} else {
		m[k] = -1
	}
	if _, ok := m[k]; ok {
		m[k]--
	} else {
		m[k] = 1
	}
	if _, ok := f[k]; ok {
		f[k]++
	} else {
		f[k] = 1
	}
	if _, ok := f[k]; ok {
		f[k]++
	} else {
		f[k] = 1.0
	}
	if _, ok := u[k]; ok {
		u[k]++
	} else {
		u[k] = 1
	}
	if _, ok := c[k]; ok {
		c[k]++
	} else {
		c[k] = 1
	}
	if _, ok := m[k]; ok {
		m[k]++
	} else {
		m["other"] = 1
	}
}

func generic[T ~int | ~float64](m map[string]T, k string, v T) {
	if _, ok := m[k]; ok {
		m[k] += v
	} else {
		m[k] = v
	}
	if _, ok := m[k]; ok {
		m[k]++
	} else {
		m[k] = 1
	}
}

func genericAppend[S ~[]E, E any](m map[string]S, k string, e E) {
	if _, ok := m[k]; ok {
		m[k] = append(m[k], e)
	} else {
		m[k] = S{e}
	}
}

func function() func() {
	m := map[string]int{}
	return func() {
		if _, ok := m["k"]; ok {
			m["k"]++
		} else {
			m["k"] = 1
		}
	}
}
