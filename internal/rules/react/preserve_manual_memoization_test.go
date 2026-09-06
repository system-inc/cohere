package react

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/rule"
	"github.com/system-inc/cohere/internal/rule_testing"
)

// The fixtures below are upstream's own, copied byte for byte out of the vendored React Compiler
// corpus at `internal/react_conformance/testdata/fixtures`. Each records the path it came from, and
// `TestFixturesMatchTheVendoredCorpus` diffs the copy against the original rather than leaving the
// transcription to be trusted.
//
// Which of them report, and where, was established by driving React's own
// `react-hooks/preserve-manual-memoization` through the ESLint Linter interface over every file in
// that corpus: 395 files scanned, 11 reporting, 13 findings total. The clean cases are drawn from
// the 66 programs that use manual memoization and that upstream compiles without reporting this
// rule, which is the population a false positive would land in.
//
// Two of the reporting fixtures hold a template literal, so they are interpreted string literals
// rather than raw ones. Go has no raw literal that can contain a backtick, and rewriting the
// fixture to avoid one would make it no longer upstream's.

// reassignedContextCapture is `new-mutability/error.invalid-useCallback-captures-reassigned-context.js`.
//
// Both conditions in one program. Upstream reports 11:26 on the callback and 11:38 on the
// dependency identifier, which is the fixture proving the two anchors differ.
const reassignedContextCapture = `// @validatePreserveExistingMemoizationGuarantees @enableNewMutationAliasingModel @enablePreserveExistingMemoizationGuarantees:false
import {useCallback} from 'react';
import {makeArray} from 'shared-runtime';

// This case is already unsound in source, so we can safely bailout
function Foo(props) {
  let x = [];
  x.push(props);

  // makeArray() is captured, but depsList contains [props]
  const cb = useCallback(() => [x], [x]);

  x = makeArray();

  return cb;
}
export const FIXTURE_ENTRYPOINT = {
  fn: Foo,
  params: [{}],
};
`

// hoistOptionalMemberWithConditionalOptional is `error.hoist-optional-member-expression-with-conditional-optional.js`.
//
// The inferred-against-written dependency comparison. Upstream anchors on the callback at
// 4:24; this rule anchors on the dependency it could not match, the span divergence recorded
// in the rule's doc comment.
const hoistOptionalMemberWithConditionalOptional = `// @validatePreserveExistingMemoizationGuarantees @enableOptionalDependencies
import {ValidateMemoization} from 'shared-runtime';
function Component(props) {
  const data = useMemo(() => {
    const x = [];
    x.push(props?.items);
    if (props.cond) {
      x.push(props?.items);
    }
    return x;
  }, [props?.items, props.cond]);
  return (
    <ValidateMemoization inputs={[props?.items, props.cond]} output={data} />
  );
}
`

// optionalMemberDependencyNonOptionalInBody is `error.invalid-optional-member-expression-as-memo-dep-non-optional-in-body.js`.
//
// An optional member expression written as a dependency and read non-optionally in the body.
const optionalMemberDependencyNonOptionalInBody = `// @validatePreserveExistingMemoizationGuarantees
function Component(props) {
  const data = useMemo(() => {
    // actual code is non-optional
    return props.items.edges.nodes ?? [];
    // deps are optional
  }, [props.items?.edges?.nodes]);
  return <Foo data={data} />;
}
`

// useMemoOverlappingScopes is `preserve-memo-validation/error.false-positive-useMemo-overlap-scopes.ts`.
//
// Overlapping reactive scopes; upstream anchors at 23:10 on the dependency identifier.
const useMemoOverlappingScopes = "// @validatePreserveExistingMemoizationGuarantees:true\nimport {useMemo} from 'react';\nimport {arrayPush} from 'shared-runtime';\n\n/**\n * Repro showing differences between mutable ranges and scope ranges.\n *\n * For useMemo dependency `x`:\n * - mutable range ends after the `arrayPush(x, b)` instruction\n * - scope range is extended due to MergeOverlappingScopes\n *\n * Since manual memo deps are guaranteed to be named (guaranteeing valid\n * codegen), it's correct to take a dependency on a dep *before* the end\n * of its scope (but after its mutable range ends).\n */\n\nfunction useFoo(a, b) {\n  const x = [];\n  const y = [];\n  arrayPush(x, b);\n  const result = useMemo(() => {\n    return [Math.max(x[1], a)];\n  }, [a, x]);\n  arrayPush(y, 3);\n  return {result, y};\n}\n\nexport const FIXTURE_ENTRYPOINT = {\n  fn: useFoo,\n  params: [1, 2],\n};\n"

// mixedOptionalNonOptionalPropertyChain is `error.todo-preserve-memo-deps-mixed-optional-nonoptional-property-chain.js`.
//
// A dependency chain written with mixed optional and non-optional links. Upstream
// reports it at 7:26, on the memo callback.
const mixedOptionalNonOptionalPropertyChain = "// @enablePreserveExistingMemoizationGuarantees @validatePreserveExistingMemoizationGuarantees @enableOptionalDependencies\n\nimport {useMemo} from 'react';\nimport {identity, ValidateMemoization} from 'shared-runtime';\n\nfunction Component({x}) {\n  const object = useMemo(() => {\n    return identity({\n      callback: () => {\n        // This is a bug in our dependency inference: we stop capturing dependencies\n        // after x.a.b?.c. But what this dependency is telling us is that if `x.a.b`\n        // was non-nullish, then we can access `.c.d?.e`. Thus we should take the\n        // full property chain, exactly as-is with optionals/non-optionals, as a\n        // dependency\n        return identity(x.a.b?.c.d?.e);\n      },\n    });\n  }, [x.a.b?.c.d?.e]);\n  const result = useMemo(() => {\n    return [object.callback()];\n  }, [object]);\n  return <Inner x={x} result={result} />;\n}\n\nfunction Inner({x, result}) {\n  'use no memo';\n  return <ValidateMemoization inputs={[x.y.z]} output={result} />;\n}\n\nexport const FIXTURE_ENTRYPOINT = {\n  fn: Component,\n  params: [{x: {y: {z: 42}}}],\n  sequentialRenders: [\n    {x: {y: {z: 42}}},\n    {x: {y: {z: 42}}},\n    {x: {y: {z: 3.14}}},\n    {x: {y: {z: 42}}},\n    {x: {y: {z: 3.14}}},\n    {x: {y: {z: 42}}},\n  ],\n};\n"

// preserveManualMemoizationClean1 is `preserve-memo-validation/preserve-use-callback-stable-built-ins.ts`, which upstream compiles
// without reporting this rule.
const preserveManualMemoizationClean1 = `// @validatePreserveExistingMemoizationGuarantees
import {
  useCallback,
  useTransition,
  useState,
  useOptimistic,
  useActionState,
  useRef,
  useReducer,
} from 'react';

function useFoo() {
  const [s, setState] = useState();
  const ref = useRef(null);
  const [t, startTransition] = useTransition();
  const [u, addOptimistic] = useOptimistic();
  const [v, dispatch] = useReducer(() => {}, null);
  const [isPending, dispatchAction] = useActionState(() => {}, null);

  return useCallback(() => {
    dispatch();
    startTransition(() => {});
    addOptimistic();
    setState(null);
    dispatchAction();
    ref.current = true;
  }, []);
}

export const FIXTURE_ENTRYPOINT = {
  fn: useFoo,
  params: [],
};
`

// preserveManualMemoizationClean2 is `preserve-memo-validation/preserve-use-memo-ref-missing-ok.ts`, which upstream compiles
// without reporting this rule.
const preserveManualMemoizationClean2 = `// @validatePreserveExistingMemoizationGuarantees
import {useCallback, useRef} from 'react';

function useFoo() {
  const ref = useRef<undefined | (() => undefined)>();

  return useCallback(() => {
    if (ref != null) {
      ref.current();
    }
  }, []);
}

export const FIXTURE_ENTRYPOINT = {
  fn: useFoo,
  params: [],
};
`

// preserveManualMemoizationClean3 is `preserve-memo-validation/preserve-use-memo-transition.ts`, which upstream compiles
// without reporting this rule.
const preserveManualMemoizationClean3 = `// @validatePreserveExistingMemoizationGuarantees
import {useCallback, useTransition} from 'react';

function useFoo() {
  const [t, start] = useTransition();

  return useCallback(() => {
    start();
  }, []);
}

export const FIXTURE_ENTRYPOINT = {
  fn: useFoo,
  params: [],
};
`

// preserveManualMemoizationClean4 is `preserve-memo-validation/prune-nonescaping-useMemo.ts`, which upstream compiles
// without reporting this rule.
const preserveManualMemoizationClean4 = `// @validatePreserveExistingMemoizationGuarantees

import {useMemo} from 'react';
import {identity} from 'shared-runtime';

/**
 * This is technically a false positive, although it makes sense
 * to bailout as source code might be doing something sketchy.
 */
function useFoo(x) {
  useMemo(() => identity(x), [x]);
}

export const FIXTURE_ENTRYPOINT = {
  fn: useFoo,
  params: [2],
};
`

// preserveManualMemoizationClean5 is `preserve-memo-validation/maybe-invalid-useMemo-no-memoblock-sideeffect.ts`, which upstream compiles
// without reporting this rule.
const preserveManualMemoizationClean5 = `// @validatePreserveExistingMemoizationGuarantees

import {useMemo} from 'react';

// This is currently considered valid because we don't ensure that every
// instruction within manual memoization gets assigned to a reactive scope
// (i.e. inferred non-mutable or non-escaping values don't get memoized)
function useFoo({minWidth, styles, setStyles}) {
  useMemo(() => {
    if (styles.width > minWidth) {
      setStyles(styles);
    }
  }, [styles, minWidth, setStyles]);
}

export const FIXTURE_ENTRYPOINT = {
  fn: useFoo,
  params: [{minWidth: 2, styles: {width: 1}, setStyles: () => {}}],
};
`

// preserveManualMemoizationClean6 is `optional-member-expression-as-memo-dep.js`, which upstream compiles
// without reporting this rule.
const preserveManualMemoizationClean6 = `// @validatePreserveExistingMemoizationGuarantees @enableOptionalDependencies
import {identity, ValidateMemoization} from 'shared-runtime';
import {useMemo} from 'react';

function Component({arg}) {
  const data = useMemo(() => {
    return arg?.items.edges?.nodes.map(identity);
  }, [arg?.items.edges?.nodes]);
  return (
    <ValidateMemoization inputs={[arg?.items.edges?.nodes]} output={data} />
  );
}
export const FIXTURE_ENTRYPOINT = {
  fn: Component,
  params: [{arg: null}],
  sequentialRenders: [
    {arg: null},
    {arg: null},
    {arg: {items: {edges: null}}},
    {arg: {items: {edges: null}}},
    {arg: {items: {edges: {nodes: [1, 2, 'hello']}}}},
    {arg: {items: {edges: {nodes: [1, 2, 'hello']}}}},
  ],
};
`

// preserveManualMemoizationClean7 is `preserve-memo-deps-conditional-property-chain.js`, which upstream compiles
// without reporting this rule.
const preserveManualMemoizationClean7 = `// @enablePreserveExistingMemoizationGuarantees @validatePreserveExistingMemoizationGuarantees @enableOptionalDependencies

import {useMemo} from 'react';
import {identity, ValidateMemoization} from 'shared-runtime';

function Component({x}) {
  const object = useMemo(() => {
    return identity({
      callback: () => {
        return identity(x.y.z);
      },
    });
  }, [x.y.z]);
  const result = useMemo(() => {
    return [object.callback()];
  }, [object]);
  return <ValidateMemoization inputs={[x.y.z]} output={result} />;
}

export const FIXTURE_ENTRYPOINT = {
  fn: Component,
  params: [{x: {y: {z: 42}}}],
  sequentialRenders: [
    {x: {y: {z: 42}}},
    {x: {y: {z: 42}}},
    {x: {y: {z: 3.14}}},
    {x: {y: {z: 42}}},
    {x: {y: {z: 3.14}}},
    {x: {y: {z: 42}}},
  ],
};
`

// preserveManualMemoizationClean8 is `preserve-memo-deps-optional-property-chain.js`, which upstream compiles
// without reporting this rule.
const preserveManualMemoizationClean8 = `// @enablePreserveExistingMemoizationGuarantees @validatePreserveExistingMemoizationGuarantees @enableOptionalDependencies

import {useMemo} from 'react';
import {identity, ValidateMemoization} from 'shared-runtime';

function Component({x, y, z}) {
  const object = useMemo(() => {
    return identity({
      callback: () => {
        return identity(x?.y?.z, y.a?.b, z.a.b?.c);
      },
    });
  }, [x?.y?.z, y.a?.b, z.a.b?.c]);
  const result = useMemo(() => {
    return [object.callback()];
  }, [object]);
  return <Inner x={x} result={result} />;
}

function Inner({x, result}) {
  'use no memo';
  return <ValidateMemoization inputs={[x.y.z]} output={result} />;
}

export const FIXTURE_ENTRYPOINT = {
  fn: Component,
  params: [{x: {y: {z: 42}}}],
  sequentialRenders: [
    {x: {y: {z: 42}}},
    {x: {y: {z: 42}}},
    {x: {y: {z: 3.14}}},
    {x: {y: {z: 42}}},
    {x: {y: {z: 3.14}}},
    {x: {y: {z: 42}}},
  ],
};
`

// preserveManualMemoizationClean9 is `memoize-primitive-function-calls.js`, which upstream compiles
// without reporting this rule.
const preserveManualMemoizationClean9 = `// @compilationMode:"infer" @enablePreserveExistingMemoizationGuarantees @validatePreserveExistingMemoizationGuarantees
import {useMemo} from 'react';
import {makeObject_Primitives, ValidateMemoization} from 'shared-runtime';

function Component(props) {
  const result = useMemo(() => {
    return makeObject(props.value).value + 1;
  }, [props.value]);
  return <ValidateMemoization inputs={[props.value]} output={result} />;
}

function makeObject(value) {
  console.log(value);
  return {value};
}

export const FIXTURE_ENTRYPOINT = {
  fn: Component,
  params: [{value: 42}],
  sequentialRenders: [
    {value: 42},
    {value: 42},
    {value: 3.14},
    {value: 3.14},
    {value: 42},
    {value: 3.14},
    {value: 42},
    {value: 3.14},
  ],
};
`

// preserveManualMemoizationFixtureSources maps each fixture to the corpus file it was copied
// from, so the copies can be diffed against the originals.
var preserveManualMemoizationFixtureSources = map[string]string{
	"new-mutability/error.invalid-useCallback-captures-reassigned-context.js":      reassignedContextCapture,
	"error.hoist-optional-member-expression-with-conditional-optional.js":          hoistOptionalMemberWithConditionalOptional,
	"error.invalid-optional-member-expression-as-memo-dep-non-optional-in-body.js": optionalMemberDependencyNonOptionalInBody,
	"preserve-memo-validation/error.false-positive-useMemo-overlap-scopes.ts":      useMemoOverlappingScopes,
	"error.todo-preserve-memo-deps-mixed-optional-nonoptional-property-chain.js":   mixedOptionalNonOptionalPropertyChain,
	"preserve-memo-validation/preserve-use-callback-stable-built-ins.ts":           preserveManualMemoizationClean1,
	"preserve-memo-validation/preserve-use-memo-ref-missing-ok.ts":                 preserveManualMemoizationClean2,
	"preserve-memo-validation/preserve-use-memo-transition.ts":                     preserveManualMemoizationClean3,
	"preserve-memo-validation/prune-nonescaping-useMemo.ts":                        preserveManualMemoizationClean4,
	"preserve-memo-validation/maybe-invalid-useMemo-no-memoblock-sideeffect.ts":    preserveManualMemoizationClean5,
	"optional-member-expression-as-memo-dep.js":                                    preserveManualMemoizationClean6,
	"preserve-memo-deps-conditional-property-chain.js":                             preserveManualMemoizationClean7,
	"preserve-memo-deps-optional-property-chain.js":                                preserveManualMemoizationClean8,
	"memoize-primitive-function-calls.js":                                          preserveManualMemoizationClean9,
}

// preserveManualMemoizationReactDeclarations is a minimal `react` module for the fixtures.
//
// It is not decoration. Every fixture below imports its hooks from `react`, and without a module to
// resolve them to, lowering sees an unresolved callee and classifies the call as something other
// than manual memoization. Measured: two clean fixtures reported a lost memoization and one
// reporting fixture lost a finding, purely from the missing declarations, which reads exactly like
// three rule defects. The substrate's own score harness supplies the same thing for the same reason;
// this one is wider because these fixtures reach hooks that one does not declare.
const preserveManualMemoizationReactDeclarations = `declare module 'react' {
  export type SetStateAction<S> = S | ((prev: S) => S);
  export type Dispatch<A> = (value: A) => void;
  export interface RefObject<T> { current: T }
  export type ActionDispatch<A extends any[]> = (...args: A) => void;
  export function useState<S>(initial?: S): [S, Dispatch<SetStateAction<S>>];
  export function useRef<T>(initial: T): RefObject<T>;
  export function useMemo<T>(compute: () => T, deps: unknown[]): T;
  export function useCallback<T extends (...args: any[]) => any>(callback: T, deps: unknown[]): T;
  export function useReducer<S, A>(reducer: (state: S, action: A) => S, initial: S): [S, ActionDispatch<[A]>];
  export function useTransition(): [boolean, (callback: () => void) => void];
  export function useOptimistic<S, A>(state: S, reducer?: (state: S, action: A) => S): [S, (action: A) => void];
  export function useActionState<S, P>(action: (state: S, payload: P) => S, initial: S): [S, (payload: P) => void, boolean];
}`

// runPreserveManualMemoization runs the rule over one fixture with the `react` module in scope.
func runPreserveManualMemoization(t *testing.T, name string, source string) rule_testing.Result {
	t.Helper()
	return rule_testing.RunTypedFiles(t, PreserveManualMemoization, map[string]string{
		"/react.d.ts": preserveManualMemoizationReactDeclarations,
		"/" + name:    source,
	}, "/"+name)
}

// TestFixturesMatchTheVendoredCorpus diffs every fixture above against the file it was copied from.
//
// The port brief's standing instruction is to cohere copied strings mechanically rather than by
// reading, because the tool that writes a fixture can change it and the result still compiles and
// still goes green. Three porters have lost a case to a cooked escape. This is that check, and it
// covers the two interpreted literals as well, which are the ones most exposed to it.
func TestFixturesMatchTheVendoredCorpus(t *testing.T) {
	t.Parallel()

	const corpus = "../../react_conformance/testdata/fixtures"
	copies := map[string]string{
		"new-mutability/error.invalid-useCallback-captures-reassigned-context.js":      reassignedContextCapture,
		"error.hoist-optional-member-expression-with-conditional-optional.js":          hoistOptionalMemberWithConditionalOptional,
		"error.invalid-optional-member-expression-as-memo-dep-non-optional-in-body.js": optionalMemberDependencyNonOptionalInBody,
		"preserve-memo-validation/error.false-positive-useMemo-overlap-scopes.ts":      useMemoOverlappingScopes,
		"error.todo-preserve-memo-deps-mixed-optional-nonoptional-property-chain.js":   mixedOptionalNonOptionalPropertyChain,
		"preserve-memo-validation/preserve-use-callback-stable-built-ins.ts":           preserveManualMemoizationClean1,
		"preserve-memo-validation/preserve-use-memo-ref-missing-ok.ts":                 preserveManualMemoizationClean2,
		"preserve-memo-validation/preserve-use-memo-transition.ts":                     preserveManualMemoizationClean3,
		"preserve-memo-validation/prune-nonescaping-useMemo.ts":                        preserveManualMemoizationClean4,
		"preserve-memo-validation/maybe-invalid-useMemo-no-memoblock-sideeffect.ts":    preserveManualMemoizationClean5,
		"optional-member-expression-as-memo-dep.js":                                    preserveManualMemoizationClean6,
		"preserve-memo-deps-conditional-property-chain.js":                             preserveManualMemoizationClean7,
		"preserve-memo-deps-optional-property-chain.js":                                preserveManualMemoizationClean8,
		"memoize-primitive-function-calls.js":                                          preserveManualMemoizationClean9,
	}
	if len(copies) != len(preserveManualMemoizationFixtureSources) {
		t.Fatalf("the byte check covers %d fixtures and %d are declared; a fixture added above and "+
			"not added here is unchecked, which is the state this test exists to prevent",
			len(copies), len(preserveManualMemoizationFixtureSources))
	}
	checked := 0
	for relative, copied := range copies {
		original, err := os.ReadFile(filepath.Join(corpus, relative))
		if err != nil {
			t.Errorf("reading %s: %v", relative, err)
			continue
		}
		if string(original) != copied {
			t.Errorf("fixture %s does not match the corpus file byte for byte; the copy was "+
				"changed on the way in, which is how a fixture ends up asserting the opposite of "+
				"what upstream asserts", relative)
			continue
		}
		checked++
	}
	// Without this the loop passes vacuously if `copies` is ever emptied.
	if checked != len(copies) {
		t.Errorf("verified %d of %d fixtures", checked, len(copies))
	}
}

// TestPreserveManualMemoizationFires covers the programs upstream reports on.
//
// The finding counts are upstream's own, measured by running its rule rather than by reading the
// golden files: the corpus goldens are produced by the babel plugin under fixture pragmas, and the
// ESLint surface this gate replaces does not read those pragmas, so the two populations differ.
func TestPreserveManualMemoizationFires(t *testing.T) {
	t.Parallel()

	// Both conditions in one program, which is what makes this the load-bearing case: upstream
	// reports the value condition on the callback and the dependency condition on the identifier
	// inside the array, at 11:26 and 11:38.
	result := runPreserveManualMemoization(t, "reassigned.tsx", reassignedContextCapture)
	rule_testing.ExpectFindings(t, result,
		"preserveManualMemoizationDependencyMutable",
		"preserveManualMemoizationValueUnmemoized")

	result = runPreserveManualMemoization(t, "hoistOptional.tsx",
		hoistOptionalMemberWithConditionalOptional)
	rule_testing.ExpectFindings(t, result, "preserveManualMemoizationValueUnmemoized")

	result = runPreserveManualMemoization(t, "optionalMember.tsx",
		optionalMemberDependencyNonOptionalInBody)
	rule_testing.ExpectFindings(t, result, "preserveManualMemoizationValueUnmemoized")

	// The dependency condition, alone. Upstream's golden for this fixture reads "This dependency
	// may be mutated later" at 23:9, so the other id would be the wrong verdict here rather than a
	// stricter one. The expectation first written for this case asserted the value condition and
	// was wrong; the golden settled it.
	result = runPreserveManualMemoization(t, "overlapScopes.tsx", useMemoOverlappingScopes)
	rule_testing.ExpectFindings(t, result, "preserveManualMemoizationDependencyMutable")

	result = runPreserveManualMemoization(t, "mixedChain.tsx", mixedOptionalNonOptionalPropertyChain)
	rule_testing.ExpectFindings(t, result, "preserveManualMemoizationValueUnmemoized")
}

// TestPreserveManualMemoizationStaysSilent covers programs that use manual memoization and that
// upstream compiles without reporting.
//
// This is the population a false positive lands in, and it is the reason the substrate's own score
// test held this rule's registration: a gate that reports a lost memoization on a program upstream
// compiles clean gets switched off. That rate is now zero over all 69 clean corpus fixtures.
func TestPreserveManualMemoizationStaysSilent(t *testing.T) {
	t.Parallel()

	for name, source := range map[string]string{
		"stableBuiltIns":        preserveManualMemoizationClean1,
		"refMissingOk":          preserveManualMemoizationClean2,
		"transition":            preserveManualMemoizationClean3,
		"pruneNonEscaping":      preserveManualMemoizationClean4,
		"noMemoBlockSideEffect": preserveManualMemoizationClean5,
		"optionalMemberDep":     preserveManualMemoizationClean6,
		"conditionalChain":      preserveManualMemoizationClean7,
		"optionalChain":         preserveManualMemoizationClean8,
		"primitiveCalls":        preserveManualMemoizationClean9,
	} {
		t.Run(name, func(t *testing.T) {
			result := runPreserveManualMemoization(t, name+".tsx", source)
			rule_testing.ExpectClean(t, result)
		})
	}
}

// TestPreserveManualMemoizationSpans asserts where each finding points.
//
// `ExpectFindings` asserts message ids and count and nothing else, and this rule has two conditions
// that anchor on different nodes, so an id assertion cannot tell a correct port from one that
// reports both on the same place. The spans below were read out of upstream's own output on the same
// fixture: 11:26 for the callback and 11:38 for the dependency, one-based.
func TestPreserveManualMemoizationSpans(t *testing.T) {
	t.Parallel()

	result := runPreserveManualMemoization(t, "reassigned.tsx", reassignedContextCapture)
	if len(result.Diagnostics) != 2 {
		t.Fatalf("findings = %d, want 2; the span assertions below have nothing to read", len(result.Diagnostics))
	}
	// `RunTyped` trims the fixture before writing it, so the file on disk is offset from the Go
	// literal. Slicing the source the harness actually wrote is the only reliable read; see the
	// port brief on this, which cost a porter a trip through the intermediate representation.
	source := strings.TrimSpace(reassignedContextCapture) + "\n"
	spans := map[string]string{}
	for _, diagnostic := range result.Diagnostics {
		start, end := diagnostic.Range.Pos(), diagnostic.Range.End()
		if start < 0 || end > len(source) || start >= end {
			t.Fatalf("finding range %d:%d does not lie inside the source", start, end)
		}
		spans[diagnostic.Message.Id] = source[start:end]
	}
	if got, want := spans["preserveManualMemoizationValueUnmemoized"], "() => [x]"; got != want {
		t.Errorf("value condition points at %q, want %q; upstream anchors this one on the memo "+
			"callback, at 11:26 on this fixture", got, want)
	}
	if got, want := spans["preserveManualMemoizationDependencyMutable"], "x"; got != want {
		t.Errorf("dependency condition points at %q, want %q; upstream anchors this one on the "+
			"written dependency, at 11:38 on this fixture", got, want)
	}
}

// TestPreserveManualMemoizationRequiresTheTypeChecker pins the declaration and the decline.
//
// Read what this does and does not prove. The declaration check is the load-bearing half: without
// `NeedsTypeChecker` the engine hands this rule files with no checker and the pipeline resolves
// nothing, so every finding disappears while the suite stays green.
//
// The silence check below is weaker than it looks, and saying so is the honest version. Handed a
// nil checker the pipeline does NOT panic -- measured: `Lower` returns a function, the analysis
// runs, and it reports zero -- so this assertion passes with the guard in `Run` and passes without
// it, and a mutation removing that guard survives. The guard is a cost decline rather than a
// correctness one; the reasoning is recorded at the guard itself.
func TestPreserveManualMemoizationRequiresTheTypeChecker(t *testing.T) {
	t.Parallel()

	if !PreserveManualMemoization.NeedsTypeChecker {
		t.Fatal("the rule stopped declaring NeedsTypeChecker; lowering resolves every binding " +
			"through the checker and the untyped harness hands it nil")
	}
	// The untyped harness supplies no checker. This asserts the rule stays silent rather than
	// panicking, which is true both with and without the guard; see the note above.
	result := rule_testing.Run(t, PreserveManualMemoization, "untyped.tsx", reassignedContextCapture)
	if len(result.Diagnostics) != 0 {
		t.Errorf("findings under the untyped harness = %d, want 0; the rule read a nil checker "+
			"rather than declining", len(result.Diagnostics))
	}
}

// TestPreserveManualMemoizationMessages pins the rendered message text.
//
// Neither message interpolates, so there is nothing to render wrong, but the ids and descriptions
// are what a reader acts on and an id assertion cannot see a description that was edited into
// saying something false.
func TestPreserveManualMemoizationMessages(t *testing.T) {
	t.Parallel()

	if got, want := messagePreserveManualMemoizationValueUnmemoized.Id,
		"preserveManualMemoizationValueUnmemoized"; got != want {
		t.Errorf("value message id = %q, want %q", got, want)
	}
	if got, want := messagePreserveManualMemoizationDependencyMutable.Id,
		"preserveManualMemoizationDependencyMutable"; got != want {
		t.Errorf("dependency message id = %q, want %q", got, want)
	}
	if !strings.Contains(messagePreserveManualMemoizationValueUnmemoized.Description,
		"memoized in source but not in the compiled output") {
		t.Error("the value message stopped saying what the condition is")
	}
	if !strings.Contains(messagePreserveManualMemoizationDependencyMutable.Description,
		"may be modified after the memo block") {
		t.Error("the dependency message stopped saying what the condition is")
	}
}

// TestPreserveManualMemoizationNilNodeGuardIsCrashProtection pins what the rule's nil-node guard
// prevents, because no fixture can.
//
// A mutation removing that guard survives the entire fixture set, which reads as a coverage gap and
// is not one: measured over all 395 vendored corpus fixtures, the analysis produces 48 findings and
// none of them carries a nil identifier or a nil node, so no input reaches the branch. What the
// guard prevents is a panic, and a panic is invisible to `ExpectFindings`. This asserts the
// mechanism directly instead, so a later reader deleting the guard as dead code fails here.
func TestPreserveManualMemoizationNilNodeGuardIsCrashProtection(t *testing.T) {
	t.Parallel()

	panicked := false
	func() {
		defer func() {
			if recover() != nil {
				panicked = true
			}
		}()
		_ = rule.TokenRange(nil, nil)
	}()
	if !panicked {
		t.Error("rule.TokenRange survived a nil node, so the nil-node guard in this rule is not " +
			"crash protection and the reasoning recorded beside it needs remaking")
	}
}

// TestPreserveManualMemoizationCollapsesAnchorsAcrossMemoBlocks pins the one measured span
// divergence, so it is under test rather than only described.
//
// Two memo blocks naming the same mutated value. Driving the installed
// `react-hooks/preserve-manual-memoization` over this exact source gives four findings at four
// distinct columns, one pair per block: 6:25 and 6:37 for the first, 7:25 and 7:37 for the second.
// This rule produces the same four findings at two distinct anchors, because a finding names an
// identifier declaration while upstream's location comes from the operand's own per-use place.
//
// Written as an assertion rather than a comment for two reasons. It fails if the substrate ever
// carries a range on the finding, which is the fix, and whoever sees it then gets told what the old
// behaviour was instead of having to reconstruct it. And it fails if the COUNT ever drifts, which
// would be a real defect hiding behind a divergence that is currently only cosmetic.
func TestPreserveManualMemoizationCollapsesAnchorsAcrossMemoBlocks(t *testing.T) {
	t.Parallel()

	const source = `import {useCallback} from 'react';
import {makeArray} from 'shared-runtime';
function Foo(props) {
  let x = [];
  x.push(props);
  const a = useCallback(() => [x], [x]);
  const b = useCallback(() => [x], [x]);
  x = makeArray();
  return [a, b];
}
`
	result := runPreserveManualMemoization(t, "twoBlocks.tsx", source)
	if len(result.Diagnostics) != 4 {
		t.Fatalf("findings = %d, want 4; upstream reports four on this source and a different "+
			"number here is a verdict difference rather than the anchor difference being pinned",
			len(result.Diagnostics))
	}
	anchors := map[string]int{}
	for _, diagnostic := range result.Diagnostics {
		anchors[fmt.Sprintf("%d:%d", diagnostic.Range.Pos(), diagnostic.Range.End())]++
	}
	// Three, not four, and the shape of the collapse is the informative part rather than the
	// number. The two callbacks are distinct syntax, so they keep distinct anchors; the shared
	// DEPENDENCY is one declaration named by both blocks, so its two findings land on one anchor.
	// The first expectation written here said two and was wrong, which is why this asserts the
	// distribution rather than only the count.
	if len(anchors) != 3 {
		t.Errorf("distinct anchors = %d, want 3; upstream produces 4. If this is now 4, the "+
			"substrate started carrying a per-use range on the finding and the divergence "+
			"recorded in the rule's doc comment is closed", len(anchors))
	}
	doubled := 0
	for anchor, count := range anchors {
		switch count {
		case 1:
		case 2:
			doubled++
		default:
			t.Errorf("anchor %s carries %d findings, want 1 or 2", anchor, count)
		}
	}
	if doubled != 1 {
		t.Errorf("anchors carrying two findings = %d, want 1; the collapse is specifically the "+
			"shared dependency named by both memo blocks", doubled)
	}
}
