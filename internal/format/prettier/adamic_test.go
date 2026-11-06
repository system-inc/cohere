package prettier

import "testing"

// The comparison engine formats an Adamic `.a` file to the same bytes as the same text named `.ts` (#6mhafvb):
// it parses with typescript, and Prettier is handed the `.ts` name, since its /\.ts$/ would otherwise keep the
// `<T,>` comma as it does for `.tsx`, which the control shows it does.
func TestTheEngineFormatsAnAdamicFileAsTypeScript(test *testing.T) {
	test.Parallel()
	engine := newTestEngine(test)
	if !engine.Handles("Dog.a") {
		test.Fatal("the engine does not handle .a")
	}
	sources := []string{
		"export const identity = <T,>(value: T): T => value;\n",
		"const widened = <number>value;\n",
		"interface Shape{width:number;height:number}\nexport function area(shape:Shape):number{return shape.width*shape.height}\n",
	}
	for _, source := range sources {
		asTypeScript, formatError := engine.Format("/repo/Dog.ts", source)
		if formatError != nil {
			test.Fatalf("as .ts: %v", formatError)
		}
		asAdamic, formatError := engine.Format("/repo/Dog.a", source)
		if formatError != nil {
			test.Fatalf("as .a: %v", formatError)
		}
		if asAdamic != asTypeScript {
			test.Errorf("%q formats as .a to\n%s\nand as .ts to\n%s", source, asAdamic, asTypeScript)
		}
	}
	asTsx, formatError := engine.Format("/repo/Dog.tsx", sources[0])
	if formatError != nil {
		test.Fatal(formatError)
	}
	asAdamic, formatError := engine.Format("/repo/Dog.a", sources[0])
	if formatError != nil {
		test.Fatal(formatError)
	}
	if asAdamic == asTsx {
		test.Errorf("the .tsx control formats `<T,>` as .a does, so the comparison above proves nothing:\n%s", asTsx)
	}
}
