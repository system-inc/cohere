package high_level_intermediate_representation

import "testing"

func TestNestedReactivityUsesCaptureIdentity(test *testing.T) {
	test.Parallel()

	for _, testCase := range []struct {
		name   string
		source string
	}{
		{
			name: "arrow with unrelated parameter",
			source: `function Component(reactiveValue: number) {
const stableValue = 42;
return (argument: number) => [reactiveValue, stableValue, argument];
}`,
		},
		{
			name: "capture through two function boundaries",
			source: `function Component(reactiveValue: number) {
const stableValue = 42;
return () => (argument: number) => [reactiveValue, stableValue, argument];
}`,
		},
	} {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			function := lowerTypedFunctions(test, "captures.tsx", testCase.source)[0]
			Construct(function)
			InferReactive(function, nil)
			var reactiveCaptures int
			var stableCaptures int
			var check func(*Function)
			check = func(parent *Function) {
				for _, instruction := range parent.Instructions {
					var nested *Function
					var captures []Place
					switch value := instruction.Value.(type) {
					case *FunctionExpression:
						nested = parent.Functions[value.Function]
						captures = value.Captures
					default:
						continue
					}
					if len(captures) != len(nested.Context) {
						test.Fatal("the fixture lost positional capture pairing")
					}
					for index, capture := range captures {
						context := nested.Context[index]
						if capture.Reactive {
							reactiveCaptures++
						} else {
							stableCaptures++
						}
						if context.Reactive != capture.Reactive {
							test.Errorf("capture %s: parent id=%d reactive=%t, child id=%d reactive=%t",
								parent.Identifier(capture.Identifier).Name, capture.Identifier,
								capture.Reactive, context.Identifier, context.Reactive)
						}
					}
					for _, parameter := range nested.Parameters {
						if parameter.Reactive {
							test.Errorf(
								"nested parameter id=%d inherited unrelated outer reactivity",
								parameter.Identifier,
							)
						}
					}
					check(nested)
				}
			}
			check(function)
			if reactiveCaptures == 0 || stableCaptures == 0 {
				test.Fatalf(
					"fixture requires both capture kinds, got reactive=%d stable=%d",
					reactiveCaptures,
					stableCaptures,
				)
			}
		})
	}
}
