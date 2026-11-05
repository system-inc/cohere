package rule

import (
	"crypto/sha256"
	"reflect"
	"strings"
	"testing"
)

// The duplicate panic is the whole reason this collector is safe to fan out against, so it is
// proven to fire rather than assumed to. A guard nobody has seen fail is indistinguishable from one
// that cannot.
func TestRegisterRejectsADuplicateName(test *testing.T) {
	test.Parallel()
	defer func() {
		recovered := recover()
		if recovered == nil {
			test.Fatal("registering two rules under one name did not panic, so the second would have " +
				"silently replaced the first and the config would address only one of them")
		}
		message, isString := recovered.(string)
		if !isString || !strings.Contains(message, "two rules answer to") {
			test.Fatalf("panicked for some other reason: %v", recovered)
		}
	}()

	Register(Registration[NoOptions]{Rule: Rule[NoOptions]{Name: "test-duplicate-probe"}})
	Register(Registration[NoOptions]{Rule: Rule[NoOptions]{Name: "test-duplicate-probe"}})
}

// An empty name is unaddressable by the config, so it is refused at registration rather than
// discovered as a rule that never runs.
func TestRegisterRejectsAnEmptyName(test *testing.T) {
	test.Parallel()
	defer func() {
		if recover() == nil {
			test.Fatal("registering a rule with no name did not panic")
		}
	}()

	Register(Registration[NoOptions]{Rule: Rule[NoOptions]{Name: ""}})
}

// Registered sorts by name, which is what lets two runs be diffed against each other.
func TestRegisteredIsSortedByName(test *testing.T) {
	test.Parallel()
	Register(
		Registration[NoOptions]{Rule: Rule[NoOptions]{Name: "test-order-zulu"}},
		Registration[NoOptions]{Rule: Rule[NoOptions]{Name: "test-order-alpha"}},
	)

	var seen []string
	for _, entry := range Registered() {
		if strings.HasPrefix(entry.Name, "test-order-") {
			seen = append(seen, entry.Name)
		}
	}

	if len(seen) != 2 {
		test.Fatalf("expected both probe rules back, got %v", seen)
	}
	if seen[0] != "test-order-alpha" || seen[1] != "test-order-zulu" {
		test.Fatalf("expected name order, got %v", seen)
	}
}

// TestTraitsCarryEveryRuleField holds Traits to Rule: every field a rule declares about itself, which is every
// field but Run and ProgramFingerprint, has a Traits field of the same name, and traitsOf copies each one. A
// field added to Rule and not here would be a declaration the walk, the findings cache and the docs never see,
// since they read a rule only through its Traits.
func TestTraitsCarryEveryRuleField(test *testing.T) {
	test.Parallel()
	subject := Rule[NoOptions]{
		ProgramFingerprint: func(Program, Given[NoOptions]) [sha256.Size]byte { return [sha256.Size]byte{} },
	}
	subjectValue := reflect.ValueOf(&subject).Elem()
	traitsType := reflect.TypeFor[Traits]()

	var carried []string
	for index := range subjectValue.NumField() {
		name := subjectValue.Type().Field(index).Name
		if name == "Run" || name == "ProgramFingerprint" {
			continue
		}
		if _, declared := traitsType.FieldByName(name); !declared {
			test.Errorf("Rule.%s has no field in Traits, so nothing past registration can read it", name)
			continue
		}
		// Each field is set to something other than its zero value, so a traitsOf that skips it reads zero and
		// differs below.
		field := subjectValue.Field(index)
		switch field.Kind() {
		case reflect.String:
			field.SetString("probe " + name)
		case reflect.Bool:
			field.SetBool(true)
		case reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uint:
			field.SetUint(1)
		case reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Int:
			field.SetInt(1)
		default:
			test.Fatalf("Rule.%s is a %s, which this test does not know how to set; teach it", name, field.Kind())
		}
		carried = append(carried, name)
	}
	if len(carried) == 0 {
		test.Fatal("no Rule field was compared, so this test proved nothing")
	}

	traits := reflect.ValueOf(traitsOf(subject))
	for _, name := range carried {
		if !traits.FieldByName(name).Equal(subjectValue.FieldByName(name)) {
			test.Errorf("traitsOf does not copy Rule.%s: the rule declares %v and its Traits read %v",
				name, subjectValue.FieldByName(name), traits.FieldByName(name))
		}
	}

	// HasProgramFingerprint is the one trait that is not a copy: it says whether ProgramFingerprint is set.
	if !traitsOf(subject).HasProgramFingerprint {
		test.Error("a rule declaring a ProgramFingerprint reads HasProgramFingerprint false")
	}
	subject.ProgramFingerprint = nil
	if traitsOf(subject).HasProgramFingerprint {
		test.Error("a rule declaring no ProgramFingerprint reads HasProgramFingerprint true")
	}
}
