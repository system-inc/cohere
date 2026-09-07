package high_level_intermediate_representation

import (
	"fmt"
	"strings"
	"testing"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/lint/rule"
	"github.com/system-inc/cohere/internal/lint/testing"
)

const effectHookDeclarations = `declare module 'react' {
export function useEffect(callback: () => unknown, dependencies?: unknown[]): void;
export function useLayoutEffect(callback: () => unknown, dependencies?: unknown[]): void;
export function useInsertionEffect(callback: () => unknown, dependencies?: unknown[]): void;
export function useCallback<T>(callback: T, dependencies: unknown[]): T;
const React: {useState: typeof useState; useMemo: typeof useMemo; useCallback: typeof useCallback;
useEffect: typeof useEffect; useLayoutEffect: typeof useLayoutEffect; useInsertionEffect: typeof useInsertionEffect};
export default React;
}`

func TestEffectHookSignaturesFollowImportOrigin(t *testing.T) {
	t.Parallel()
	for _, hook := range []string{"useEffect", "useLayoutEffect", "useInsertionEffect"} {
		for _, testCase := range []struct {
			name       string
			prefix     string
			callee     string
			parameters string
			frozen     bool
		}{
			{"named", `import {%s} from 'react';`, hook, "", true},
			{"renamed", `import {%s as effect} from 'react';`, "effect", "", true},
			{"namespace", `import * as React from 'react';`, "React." + hook, "", true},
			{"default", `import React from 'react';`, "React." + hook, "", true},
			{"barrel", `import {%s as effect} from './barrel';`, "effect", "", true},
			{"shadow", `import {%s as effect} from 'react';`, "effect", "effect: (callback: unknown, dependencies: unknown[]) => unknown", false},
			{"unrelated", `import {%s as effect} from './unrelated';`, "effect", "", false},
		} {
			t.Run(hook+"/"+testCase.name, func(t *testing.T) {
				prefix := strings.ReplaceAll(testCase.prefix, "%s", hook)
				source := prefix + ` function Component(` + testCase.parameters + `) { return ` + testCase.callee + `(() => {}, []); }`
				visited := 0
				probe := rule.Rule{Name: "effect-hook-signature", NeedsTypeChecker: true, Run: func(ctx rule.Context, options any) rule.Listeners {
					return rule.Listeners{ast.KindSourceFile: func(node *ast.Node) {
						forEachFunctionLike(node, func(node *ast.Node) {
							function := Lower(node, ctx.TypeChecker)
							if function == nil {
								return
							}
							Construct(function)
							effects := InferAliasingEffects(function)
							for _, instruction := range function.Instructions {
								if instruction == nil {
									continue
								}
								switch instruction.Value.(type) {
								case *CallExpression, *MethodCall:
								default:
									continue
								}
								visited++
								var origin ModuleExportOrigin
								switch call := instruction.Value.(type) {
								case *CallExpression:
									origin = call.CalleeOrigin
								case *MethodCall:
									origin = call.CalleeOrigin
								}
								if (origin.Module == "react" && origin.Export == hook) != testCase.frozen {
									t.Errorf("origin=%+v; want React hook=%t", origin, testCase.frozen)
								}
								frozen := 0
								result := EffectValueMutable
								for _, effect := range effects.Get(instruction.Id) {
									if effect.Kind == AliasingEffectFreeze {
										frozen++
									}
									if effect.Kind == AliasingEffectCreate && effect.Into.Identifier == instruction.LValue.Identifier {
										result = effect.Value
									}
								}
								wantFrozen := 0
								wantResult := EffectValueMutable
								if testCase.frozen || testCase.name == "unrelated" {
									wantFrozen = 2
									wantResult = EffectValueFrozen
									if hook == "useEffect" && testCase.frozen {
										wantResult = EffectValuePrimitive
									}
								}
								if frozen != wantFrozen || result != wantResult {
									t.Errorf("frozen arguments=%d result=%v, want %d and %v", frozen, result, wantFrozen, wantResult)
								}
							}
						})
					}}
				}}
				rule_testing.RunTypedFiles(t, probe, map[string]string{
					"/react.d.ts":   reactiveDeclarations + effectHookDeclarations,
					"/barrel.ts":    fmt.Sprintf("export {%s} from 'react';", hook),
					"/unrelated.ts": fmt.Sprintf("export function %s(callback: unknown, dependencies: unknown[]) {return {callback, dependencies};}", hook),
					"/fixture.tsx":  source,
				}, "/fixture.tsx")
				if visited != 1 {
					t.Fatalf("visited %d calls, want one", visited)
				}
			})
		}
	}
}

func TestEffectHookDoesNotPromoteStableSetter(t *testing.T) {
	t.Parallel()
	const source = `import React from 'react'; import {useNotifications} from './hooks';
function Component(props: {items?: number[]}) {
const notifications = useNotifications();
const [value, setValue] = React.useState(false);
const propItems = props.items;
const items = React.useMemo(() => propItems?.slice().reverse() ?? [], [propItems]);
React.useEffect(() => {if (value) return; items.forEach(item => notifications.remove(item));}, [items, value, notifications]);
const callback = React.useCallback(() => setValue(true), []);
return <div onClick={callback}>{items.map(item => <span>{item}</span>)}</div>;
}`
	for _, testCase := range []struct {
		name   string
		source string
		fires  bool
	}{
		{"effect", source, false},
		{"layout", strings.ReplaceAll(source, "React.useEffect(", "React.useLayoutEffect("), false},
		{"insertion", strings.ReplaceAll(source, "React.useEffect(", "React.useInsertionEffect("), false},
		{"no effect", strings.ReplaceAll(source, "React.useEffect(() => {if (value) return; items.forEach(item => notifications.remove(item));}, [items, value, notifications]);", ""), false},
		{"unpreserved dependency", `import {useMemo} from 'react'; function Component({propA}) {return useMemo(() => propA.x(), [propA.x]);}`, true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			findings, markers := 0, 0
			probe := rule.Rule{Name: "effect-hook-memoization", NeedsTypeChecker: true, Run: func(ctx rule.Context, options any) rule.Listeners {
				return rule.Listeners{ast.KindSourceFile: func(node *ast.Node) {
					forEachFunctionLike(node, func(node *ast.Node) {
						function := Lower(node, ctx.TypeChecker)
						if function == nil {
							return
						}
						Construct(function)
						findings += len(AnalyzePreservedManualMemoization(function, ctx.TypeChecker))
						for _, instruction := range function.Instructions {
							if instruction != nil {
								if _, ok := instruction.Value.(*StartMemoize); ok {
									markers++
								}
							}
						}
					})
				}}
			}}
			rule_testing.RunTypedFiles(t, probe, map[string]string{"/react.d.ts": reactiveDeclarations + effectHookDeclarations, "/fixture.tsx": testCase.source}, "/fixture.tsx")
			if markers == 0 {
				t.Fatal("no memo markers exercised")
			}
			if (findings != 0) != testCase.fires {
				t.Fatalf("findings=%d want fires=%t", findings, testCase.fires)
			}
		})
	}
}
