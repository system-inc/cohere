package regexp

// Greek script aliases use the same pinned ranges as Adamic's regex compiler.
// regexp2 understands .NET property names, which cannot spell Script=Greek.
func greekPropertyAtoms(source string) ([]classAtom, bool) {
	var ranges [][2]rune
	switch source[3 : len(source)-1] {
	case "Script=Greek", "Script=Grek", "sc=Greek", "sc=Grek":
		ranges = greekScriptRanges
	case "Script_Extensions=Greek", "Script_Extensions=Grek", "scx=Greek", "scx=Grek":
		ranges = greekExtensionRanges
	default:
		return nil, false
	}
	atoms := make([]classAtom, 0, len(ranges)+1)
	if source[1] == 'P' {
		// Under u, complement precedes case closure. Negating an already widened
		// class would lose matches such as Greek mu folding to the common micro sign.
		next := rune(0)
		for _, span := range ranges {
			if next < span[0] {
				atoms = append(atoms, classAtom{kind: classRange, lo: next, hi: span[0] - 1})
			}
			next = span[1] + 1
		}
		if next <= 0x10ffff {
			atoms = append(atoms, classAtom{kind: classRange, lo: next, hi: 0x10ffff})
		}
	} else {
		for _, span := range ranges {
			atoms = append(atoms, classAtom{kind: classRange, lo: span[0], hi: span[1]})
		}
	}
	return atoms, true
}
