package estree

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"testing"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/system-inc/cohere/internal/format/oracle_testing"
	"github.com/system-inc/cohere/internal/json_value"
)

// convertFixtures are the constructs the converter's corpus differential reached, written small and in the
// repository: decorators, satisfies, enums, overloads and JSX, and the TypeScript around them a converter
// most easily drops or misplaces. Non-ASCII text and comments are spread through them, since the oracle's
// offsets are UTF-16 and the converter's are bytes. A named character reference in JSX text (&amp;) is not
// here: the fork's parse keeps it as written in the JSXText's value and the converter decodes it, a
// disagreement this fixture found and leaves for its own task.
var convertFixtures = []struct{ fileName, text string }{
	{"decorators.ts", `@Component({ selector: "app" })
export class Widget {
    @Input() name: string = "";
    @Output() readonly changed = new EventEmitter<string>();
    @HostListener("click", ["$event"]) onClick(event: Event) {}
    constructor(@Inject(TOKEN) private readonly service: Service, @Optional() other?: Other) {}
    @memoize get total(): number { return 1; }
    @log static create(@required value: string) { return new Widget(value as never); }
    @bound accessor count = 0;
}
@first @second.member @third() abstract class Stacked {}
export default @decorated class {}
`},
	{"satisfies.ts", `type Config = { mode: "dev" | "prod"; port: number };
const config = { mode: "dev", port: 3000 } satisfies Config;
const routes = { home: "/", about: "/about" } as const satisfies Record<string, string>;
const palette = { red: [255, 0, 0] } satisfies Record<string, [number, number, number] | string>;
export default { name: "é😀" } satisfies { name: string };
const nested = ((value satisfies unknown) as string).length;
`},
	{"enums.ts", `enum Direction { Up = 1, Down, Left, Right }
const enum Flags { None = 0, A = 1 << 0, B = 1 << 1, AB = A | B }
declare enum Ambient { X, Y }
export enum Labels { Yes = "yes", No = "no", Maybe = ` + "`maybe`" + ` }
enum Computed { Length = "abc".length, "quoted" = 2, "中文" = 3 }
export declare const enum Both { One }
`},
	{"overloads.ts", `function parse(text: string): Node;
function parse(text: string, strict: true): Node | never;
function parse(text: string, strict?: boolean): Node | undefined { return undefined; }
export function overloaded(value: number): number;
export function overloaded(value: string): string;
export function overloaded(value: unknown) { return value; }
declare function ambient(a: string): void;
declare function ambient(a: number): void;
class Overloads {
    method(): void;
    method(value: string): void;
    method(value?: string) {}
    constructor();
    constructor(value: number);
    constructor(value?: number) {}
}
interface Callable { (value: string): string; (value: number): number; new (value: string): Callable; }
`},
	{"elements.tsx", `import type { ReactNode } from "react";
export const View = <T,>({ items, render }: { items: T[]; render: (item: T) => ReactNode }) => (
    <>
        <ul className="list" {...rest} data-é="😀">
            {items.map((item, index) => <li key={index}>{render(item)}</li>)}
        </ul>
        <svg:rect width={10} />
        <Namespace.Member.Component disabled label='single' />
        {/* a comment child */}
        text and entities &#169; &#x1F600; {"string child"}
        <Generic<string> value="x" />
        {condition ? <A /> : null}
    </>
);
`},
	{"types.ts", `// leading comment with 中文 and 😀
type Mapped<T> = { readonly [K in keyof T as ` + "`get${Capitalize<string & K>}`" + `]-?: () => T[K] };
type Conditional<T> = T extends (infer U extends string)[] ? U : never;
type Tuple = [name: string, age?: number, ...rest: boolean[]];
type Template = ` + "`${number}px` | `${'a' | 'b'}-${string}`" + `;
declare const tag: unique symbol;
function assertIsString(value: unknown): asserts value is string {}
function isNumber(value: unknown): value is number { return typeof value === "number"; }
abstract class Shape implements HasArea, HasName { abstract area(): number; protected abstract readonly name: string; #secret = 1n; static { init(); } }
namespace Outer.Inner { export const value = 1; }
declare module "module-name" { export function f(): void; }
declare global { interface Window { custom: string } }
import type { A } from "./a";
import { type B, C } from "./b";
export type { D } from "./d";
export * as namespace from "./e";
const cast = <number>value;
const chained = object?.property?.[index]?.(argument)!;
let definite!: string;
const regex = /[a-z]+/giu, big = 123n;
label: for (const item of items) { if (item) continue label; else break label; }
/** JSDoc for the function */
export async function* generator<const T extends readonly unknown[]>(this: Window, ...values: T): AsyncGenerator<T[number]> {}
`},
	{"module.mts", `export const value: number = 1; /* trailing block */
import json from "./data.json" with { type: "json" };
export = value;
`},
}

// TestConvertAgreesWithPrettier is the converter's acceptance test: for every fixture, the Go tree and comment
// list must equal what the fork's own parse produces, property for property. The fork's answers are recorded
// (see oracle_testing): its bundle's prettier.__debug.parse with parser "typescript" and the fixture's file
// path, so JSX and sourceType are decided as Prettier decides them, dumped in the shape dumpValue prints, with
// offsets in UTF-16, which dumpFile converts to. The live differential ran over corpora named by
// COHERE_ESTREE_CORPORA; these fixtures hold what that corpus reached.
func TestConvertAgreesWithPrettier(test *testing.T) {
	test.Parallel()
	golden := oracle_testing.Open(test, test.Name())
	for _, fixture := range convertFixtures {
		input, marshalError := json.Marshal(struct {
			FileName string `json:"fileName"`
			Text     string `json:"text"`
		}{fixture.fileName, fixture.text})
		if marshalError != nil {
			test.Fatalf("%s: %v", fixture.fileName, marshalError)
		}
		answer, upstreamError := golden.Answer(oracle_testing.Key("typescriptParse", string(input)))
		if upstreamError != nil {
			test.Errorf("%s: the fork refused it, so it holds nothing: %v", fixture.fileName, upstreamError)
			continue
		}
		parsed, parseError := json_value.Parse([]byte(answer))
		if parseError != nil {
			test.Fatalf("%s: the oracle answered something that is not JSON: %v", fixture.fileName, parseError)
		}
		want, isObject := parsed.(json_value.Object)
		if !isObject {
			test.Fatalf("%s: the oracle answered something that is not an object", fixture.fileName)
		}
		got, dumpFileError := dumpFile(fixture.fileName, fixture.text)
		if dumpFileError != nil {
			test.Errorf("%s: the fork parses it and the converter refused: %v", fixture.fileName, dumpFileError)
			continue
		}
		for _, key := range []string{"ast", "comments"} {
			if difference := firstDifference(want[key], got[key], key); difference != "" {
				test.Errorf("%s: %s", fixture.fileName, difference)
				break
			}
		}
	}
}

// dumpFile converts one file and renders it the way the oracle script does, as the JSON values the oracle's
// line parses to, so both sides compare as the same values.
func dumpFile(path string, text string) (json_value.Object, error) {
	program, comments, _, parseTypeScriptError := ParseTypeScript(path, text, nil)
	if parseTypeScriptError != nil {
		return nil, parseTypeScriptError
	}
	offsets := utf16Offsets(text)
	dumped := make(json_value.Array, len(comments))
	for index, comment := range comments {
		dumped[index] = json_value.Object{
			"type": jsonString(comment.Type()),
			"range": json_value.Array{
				jsonInteger(offsets[comment.Range[0]]),
				jsonInteger(offsets[comment.Range[1]]),
			},
			"value": jsonString(comment.String("value")),
		}
	}
	return json_value.Object{"ast": dumpValue(program, offsets), "comments": dumped}, nil
}

// jsonString is a Go string as JSON carries it: encoding/json writes each byte that is not UTF-8 as U+FFFD,
// and converting to runes replaces each such byte the same way, so a dump holds the string it would after a
// round trip through JSON.
func jsonString(text string) json_value.Value {
	return json_value.String(string([]rune(text)))
}

// jsonInteger is an int as a JSON number.
func jsonInteger(number int) json_value.Value {
	return json_value.Number(strconv.Itoa(number))
}

// firstDifference names the first path where two JSON values differ, or "".
func firstDifference(want json_value.Value, got json_value.Value, path string) string {
	switch typed := want.(type) {
	case json_value.Object:
		other, isMap := got.(json_value.Object)
		if !isMap {
			return fmt.Sprintf("%s: want an object, got %v", path, got)
		}
		keys := map[string]bool{}
		for key := range typed {
			keys[key] = true
		}
		for key := range other {
			keys[key] = true
		}
		sorted := make([]string, 0, len(keys))
		for key := range keys {
			sorted = append(sorted, key)
		}
		sort.Strings(sorted)
		for _, key := range sorted {
			wantValue, wantPresent := typed[key]
			gotValue, gotPresent := other[key]
			if wantPresent != gotPresent {
				return fmt.Sprintf(
					"%s.%s: present in the fork %v, in Go %v (%s)",
					path,
					key,
					wantPresent,
					gotPresent,
					typed["type"],
				)
			}
			if difference := firstDifference(wantValue, gotValue, path+"."+key); difference != "" {
				return difference
			}
		}
		return ""
	case json_value.Array:
		other, isList := got.(json_value.Array)
		if !isList || len(other) != len(typed) {
			return fmt.Sprintf("%s: want %d elements, got %v", path, len(typed), got)
		}
		for index := range typed {
			if difference := firstDifference(
				typed[index],
				other[index],
				fmt.Sprintf("%s[%d]", path, index),
			); difference != "" {
				return difference
			}
		}
		return ""
	}
	if scalarText(want) != scalarText(got) {
		return fmt.Sprintf("%s: want %v, got %v", path, scalarText(want), scalarText(got))
	}
	return ""
}

// scalarText is a JSON value as fmt prints the Go value encoding/json decodes it to, which is what the
// comparison has always been: a number by its float64, so `1.0` and `1` agree, and null as `<nil>`.
func scalarText(value json_value.Value) string {
	switch typed := value.(type) {
	case json_value.Null:
		return "<nil>"
	case json_value.Boolean:
		return strconv.FormatBool(bool(typed))
	case json_value.String:
		return string(typed)
	case json_value.Number:
		// Ignored: a number out of float64's range parses to an infinity, which prints as one, and every
		// Number here was a number literal.
		number, _ := strconv.ParseFloat(string(typed), 64)
		return fmt.Sprint(number)
	case json_value.Object:
		return fmt.Sprintf("an object %v", typed)
	case json_value.Array:
		return fmt.Sprintf("an array %v", typed)
	}
	return "<missing>"
}

// utf16Offsets maps every byte offset that starts a character (and the end) to its UTF-16 index.
func utf16Offsets(text string) []int {
	offsets := make([]int, len(text)+1)
	index := 0
	for position, character := range text {
		offsets[position] = index
		size := utf8.RuneLen(character)
		for continuation := 1; continuation < size; continuation++ {
			offsets[position+continuation] = index
		}
		index += len(utf16.Encode([]rune{character}))
	}
	offsets[len(text)] = index
	return offsets
}

// dumpValue is a property's value as JSON: a node as its type, its range through offsets and its properties,
// leaving out its comments, its tokens and the properties named with a leading __ but __contentEnd.
func dumpValue(value Value, offsets []int) json_value.Value {
	switch typed := value.(type) {
	case nil:
		return json_value.Null{}
	case *Node:
		if typed == nil {
			return json_value.Null{}
		}
		result := json_value.Object{
			"type":  jsonString(typed.Type()),
			"range": json_value.Array{jsonInteger(offsets[typed.Range[0]]), jsonInteger(offsets[typed.Range[1]])},
		}
		if typed.HasContentEnd {
			result["__contentEnd"] = jsonInteger(offsets[typed.ContentEnd])
		}
		keys := typed.Keys()
		sort.Strings(keys)
		for _, key := range keys {
			if key == "comments" || key == "tokens" || strings.HasPrefix(key, "__") {
				continue
			}
			result[key] = dumpValue(typed.Get(key), offsets)
		}
		return result
	case Nodes:
		result := make(json_value.Array, len(typed))
		for index, child := range typed {
			result[index] = dumpValue(child, offsets)
		}
		return result
	case Values:
		result := make(json_value.Array, len(typed))
		for index, element := range typed {
			result[index] = dumpValue(element, offsets)
		}
		return result
	case Record:
		result := make(json_value.Object, len(typed))
		for key, member := range typed {
			result[key] = dumpValue(member, offsets)
		}
		return result
	case *TemplateValue:
		var cooked json_value.Value = json_value.Null{}
		if typed.Cooked != nil {
			cooked = jsonString(*typed.Cooked)
		}
		return json_value.Object{"cooked": cooked, "raw": jsonString(typed.Raw)}
	case *Regex:
		return json_value.Object{"flags": jsonString(typed.Flags), "pattern": jsonString(typed.Pattern)}
	case String:
		return jsonString(string(typed))
	case Boolean:
		return json_value.Boolean(typed)
	case Number:
		// NaN and the infinities have no JSON spelling; encoding/json refused them where this dump went
		// through it, and json.Marshal still refuses this Number, so a convertedDump fails alike.
		return json_value.Number(strconv.FormatFloat(float64(typed), 'g', -1, 64))
	case Integer:
		return jsonInteger(int(typed))
	}
	// Invariant: the cases above are every type that declares isValue, which only this package can.
	panic(fmt.Sprintf("estree: a property value of type %T", value))
}
