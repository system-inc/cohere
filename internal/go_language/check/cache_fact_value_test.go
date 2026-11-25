package check

import (
	"testing"
	"time"
)

// A fact's value digest is what gob carries of it: an unexported field, a func field and a slice's being nil rather
// than empty move nothing, while every exported value, a pointer's being nil, an interface's dynamic type and a map's
// entries do, whatever order the map was filled in.
func TestAFactsValueDigestIsWhatGobCarries(test *testing.T) {
	test.Parallel()

	base := func() *valueFact {
		return &valueFact{Count: 1, Names: []string{"a", "b"}, Weights: map[string]float64{"x": 1, "y": 2},
			Held: time.Duration(5), When: time.Unix(0, 0).UTC()}
	}
	digest := func(fact *valueFact) string {
		test.Helper()
		value, digestError := factValueDigest(fact)
		if digestError != nil {
			test.Fatalf("digesting %+v: %v", fact, digestError)
		}
		return string(value[:])
	}
	same := map[string]func(*valueFact){
		"an unexported field": func(fact *valueFact) { fact.hidden = 7 },
		"a func field": func(fact *valueFact) {
			fact.Callback = func() { /* Never called: only whether the field is set matters. */ }
		},
		"a map filled in another order": func(fact *valueFact) {
			fact.Weights = map[string]float64{}
			fact.Weights["y"] = 2
			fact.Weights["x"] = 1
		},
	}
	for name, change := range same {
		changed := base()
		change(changed)
		if digest(changed) != digest(base()) {
			test.Errorf("want %s to leave the digest as it was", name)
		}
	}
	empty, null := base(), base()
	empty.Names, null.Names = []string{}, nil
	if digest(empty) != digest(null) {
		test.Errorf("want a nil slice and an empty one alike, as gob sends neither")
	}
	different := map[string]func(*valueFact){
		"a number":                    func(fact *valueFact) { fact.Count = 2 },
		"a slice's element":           func(fact *valueFact) { fact.Names[1] = "c" },
		"a slice's length":            func(fact *valueFact) { fact.Names = append(fact.Names, "") },
		"a map's value":               func(fact *valueFact) { fact.Weights["x"] = 3 },
		"a map's key":                 func(fact *valueFact) { fact.Weights = map[string]float64{"x": 1, "z": 2} },
		"a pointer to a zero value":   func(fact *valueFact) { fact.Next = &valueFact{} },
		"an interface's dynamic type": func(fact *valueFact) { fact.Held = time.Month(5) },
		"a nil interface":             func(fact *valueFact) { fact.Held = nil },
		"a value gob marshals":        func(fact *valueFact) { fact.When = time.Unix(1, 0).UTC() },
	}
	for name, change := range different {
		changed := base()
		change(changed)
		if digest(changed) == digest(base()) {
			test.Errorf("want %s to move the digest", name)
		}
	}
}

// A value gob cannot carry, met where gob would meet it, is an error rather than a part left out.
func TestAFactValueGobCannotCarryIsAnError(test *testing.T) {
	test.Parallel()

	fact := &valueFact{Held: channelStringer(make(chan int))}
	if _, digestError := factValueDigest(fact); digestError == nil {
		test.Fatalf("want a chan inside an interface refused, got a digest")
	}
}
