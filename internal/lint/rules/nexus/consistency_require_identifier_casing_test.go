package nexus

import (
	"slices"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/lint/testing"
)

// identifierCasingFiles is a typed program around one subject file: a JSX namespace whose element carries a
// React element's members, a dependency with a wire interface and a base class, and Node's environment and
// file-system constants as a dependency declares them.
func identifierCasingFiles(subjectFileName string, source string) map[string]string {
	return map[string]string{
		subjectFileName: source,
		"tsconfig.json": `{ "compilerOptions": { "strict": true, "target": "ES2022", "lib": ["ES2022"], ` +
			`"types": [], "jsx": "preserve", "experimentalDecorators": true }, "include": ["**/*.ts", "**/*.tsx"] }`,
		"jsx.d.ts": "declare namespace JSX { interface Element { type: unknown; props: unknown; key: unknown } " +
			"interface IntrinsicElements { [name: string]: unknown } }\n",
		"node_modules/wire/index.d.ts": "export interface Wire { do_work(): void; unit_amount: number }\n" +
			"export declare class WireBase { handle_event(): void }\n" +
			"export declare const constants: { O_RDONLY: number; open_mode: number };\n",
		"node_modules/@types/node/index.d.ts": "declare var process: { env: Record<string, string | undefined> };\n",
	}
}

// identifierCasingFindings runs the rule over one subject file and returns the text each finding covers.
func identifierCasingFindings(test *testing.T, subjectFileName string, source string) []string {
	test.Helper()
	result := rule_testing.RunTypedFiles(
		test,
		ConsistencyRequireIdentifierCasing,
		identifierCasingFiles(subjectFileName, source),
		subjectFileName,
	)
	covered := []string{}
	for _, diagnostic := range result.Diagnostics {
		covered = append(covered, result.SourceFile.Text()[diagnostic.Range.Pos():diagnostic.Range.End()])
	}
	return covered
}

func TestConsistencyRequireIdentifierCasingFires(test *testing.T) {
	test.Parallel()

	cases := []struct {
		name   string
		source string
		want   []string
	}{
		{"a parameter", "export function sum(first_value: number) { return first_value; }\n", []string{"first_value"}},
		{
			"a let, once though it is written and read",
			"export function total() { let running_total = 0; running_total += 1; return running_total; }\n",
			[]string{"running_total"},
		},
		{
			"a var opening on a capital",
			"export function biggest() { var MaxSize = 10; return MaxSize; }\n",
			[]string{"MaxSize"},
		},
		// A constant inside a function reaches no other file, so it is a local value, judged here.
		{
			"a const inside a function",
			"export function biggest() { const MaxSize = 10; return MaxSize; }\n",
			[]string{"MaxSize"},
		},
		{
			"a const inside a block",
			"export function biggest() { if (true) { const unit_price = 1; return unit_price; } return 0; }\n",
			[]string{"unit_price"},
		},
		{"an interface", "export interface Order_Line { unitPrice: number }\n", []string{"Order_Line"}},
		{"a type alias", "export type order_line = { unitPrice: number };\n", []string{"order_line"}},
		{"a class", "export class Order_Book {}\n", []string{"Order_Book"}},
		{"an enum", "export enum order_state { Open }\n", []string{"order_state"}},
		{"a class method", "export class Book { do_work(): void {} }\n", []string{"do_work"}},
		{
			"a private method",
			"export class Book { #do_work(): void {} run(): void { this.#do_work(); } }\n",
			[]string{"#do_work"},
		},
		{"an interface method", "export interface Worker { do_work(): void }\n", []string{"do_work"}},
		{"a type literal method", "export type Worker = { do_work(): void };\n", []string{"do_work"}},
		{
			"a function",
			"export function Format_Number(value: number): string { return String(value); }\n",
			[]string{"Format_Number"},
		},
		{
			"a function opening on a capital",
			"export function FormatNumber(value: number): string { return String(value); }\n",
			[]string{"FormatNumber"},
		},
		// The base is where the name is chosen, so it is reported there and only there.
		{
			"the base of an override",
			"export class Base { do_work(): void {} }\nexport class Child extends Base { override do_work(): void {} }\n",
			[]string{"do_work"},
		},
		{
			"an interface, not the class implementing it",
			"interface Worker { do_work(): void }\nexport class Job implements Worker { do_work(): void {} }\n",
			[]string{"do_work"},
		},
		{
			"overloads, once",
			"export function parse_value(input: string): string;\nexport function parse_value(input: number): number;\nexport function parse_value(input: string | number) { return input; }\n",
			[]string{"parse_value"},
		},
		{
			"array destructuring",
			"export function first() { let [first_value] = [1]; return first_value; }\n",
			[]string{"first_value"},
		},
		{
			"a renamed element",
			"export function first() { let { a: second_value } = { a: 1 }; return second_value; }\n",
			[]string{"second_value"},
		},
		{
			"a renamed parameter element",
			"export function first({ a: first_value }: { a: number }) { return first_value; }\n",
			[]string{"first_value"},
		},
		// A mirror keeps the external system's own case, so a recased read mirrors nothing (us10).
		{
			"a recased environment read",
			"export function address() { const database_url = process.env.DATABASE_URL ?? ''; return database_url; }\n",
			[]string{"database_url"},
		},
		{
			"a dependency's property read into another spelling",
			"import { constants } from 'wire';\nexport function mode() { let read_mode = constants.open_mode; return read_mode; }\n",
			[]string{"read_mode"},
		},
		{
			"an underscore inside a component's name",
			"export function Order_List() { return <ul />; }\n",
			[]string{"Order_List"},
		},
	}
	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			if got := identifierCasingFindings(
				test,
				"subject.tsx",
				testCase.source,
			); !slices.Equal(got, testCase.want) {
				test.Errorf("reported %q, want %q", got, testCase.want)
			}
		})
	}
}

func TestConsistencyRequireIdentifierCasingStaysSilent(test *testing.T) {
	test.Parallel()

	cases := []struct {
		name   string
		source string
	}{
		{
			"camel values and upper camel types",
			"export interface OrderLine { unitPrice: number }\nexport function sum(firstValue: number) { let runningTotal = firstValue; return runningTotal; }\n",
		},
		// A module's own constant is consistency-require-constant-casing's, whose casing there signals reach.
		{
			"a module constant",
			"const MaxSize = 1;\nconst unit_price = 2;\nexport const Total = MaxSize + unit_price;\n",
		},
		{"a namespace constant", "export namespace Limits { export const max_size = 1; }\n"},
		// The shouting shape is consistency-no-screaming-snake-case's, at any scope and in any binding.
		{
			"a screaming name",
			"export function limit(MAX_SIZE: number) { let RETRY_COUNT = 3; const LOCAL_LIMIT = 2; return MAX_SIZE + RETRY_COUNT + LOCAL_LIMIT; }\n",
		},
		// Property names are wire keys in TypeScript, so none is judged.
		{
			"property names",
			"export interface Row { created_at: string; REDDIT_PIXEL_ID: string; Large: string }\nexport class Store { unit_count = 0; get total_value() { return 1; } }\nexport const payload = { unit_amount: 1, do_work() {} };\n",
		},
		{
			"a parameter property",
			"export class Store { constructor(private unit_price: number) {} total() { return this.unit_price; } }\n",
		},
		{
			"a shorthand element",
			"export function read(row: { unit_price: number }) { let { unit_price } = row; return unit_price; }\n",
		},
		// Leading and trailing underscores wait on Kirk's call, and the bare discard carries no casing.
		{
			"underscores",
			"export function run(_: number, _cache: number, __meta__: number, event_: number, _Unit_Count: number) { let _running_total = 0; return _running_total; }\n",
		},
		{"a reserved word's escape", "export function read(class_: string) { return class_; }\n"},
		{
			"a name the converter cannot advise",
			"export function read($element: number) { let $Element = $element; return $Element; }\n",
		},
		{
			"a catch binding",
			"export function run() { try { return 1; } catch (error_value) { return error_value; } }\n",
		},
		{
			"a type parameter and an enum member",
			"export function run<T_Value>(value: T_Value) { return value; }\nexport enum State { open_state }\n",
		},
		// Someone else's names.
		{
			"a dependency's method",
			"import { Wire } from 'wire';\nexport class Local implements Wire { unit_amount = 1; do_work(): void {} }\n",
		},
		{
			"a decorated method",
			"declare function HttpRoute(path: string): MethodDecorator;\nexport class Routes { @HttpRoute('/device') http_deviceId(): void {} }\n",
		},
		{
			"a dependency base's method",
			"import { WireBase } from 'wire';\nexport class Local extends WireBase { handle_event(): void {} }\n",
		},
		{
			"ambient declarations",
			"declare global { var __APP_STATE__: number; function do_thing(first_value: number): void }\ndeclare function load_thing(first_value: number): void;\nexport {};\n",
		},
		{
			"an exact environment mirror",
			"export function address() { let DatabaseUrl = process.env.DatabaseUrl ?? ''; return DatabaseUrl; }\n",
		},
		{
			"an exact dependency mirror",
			"import { constants } from 'wire';\nexport function mode() { let open_mode = constants.open_mode; return open_mode; }\n",
		},
		// Kept capitals, by structure and types.
		{"a component", "export function OrderList() { return <ul />; }\n"},
		{
			"a rendered parameter",
			"export function Shell({ icon: Icon }: { icon: () => JSX.Element }) { return <Icon />; }\n",
		},
		{"a constructor parameter", "export function build(Model: new () => object) { return new Model(); }\n"},
		{
			"a component a local holds",
			"declare function pick(): () => JSX.Element;\nexport function row() { const Icon = pick(); return Icon; }\n",
		},
		{
			"a local dynamic import",
			"export async function run() { const NodeFileSystem = await import('wire'); return NodeFileSystem; }\n",
		},
		{
			"a local shadowing its camel source",
			"declare const processingIcon: number | undefined;\nexport function row() { const ProcessingIcon = processingIcon ?? 1; return ProcessingIcon; }\n",
		},
	}
	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.RunTypedFiles(
				test,
				ConsistencyRequireIdentifierCasing,
				identifierCasingFiles("subject.tsx", testCase.source),
				"subject.tsx",
			)
			rule_testing.ExpectClean(test, result)
		})
	}
}

// Next.js reads these exports by name, so a route's `GET` is the framework's spelling, and the same name
// outside a route is judged.
func TestConsistencyRequireIdentifierCasingLeavesNextRouteContractsAlone(test *testing.T) {
	test.Parallel()

	source := "export async function GET() { return 1; }\n"
	if got := identifierCasingFindings(test, "app/api/report/route.ts", source); len(got) > 0 {
		test.Errorf("a route's GET was reported: %q", got)
	}
	if got := identifierCasingFindings(test, "source/Report.ts", source); !slices.Equal(got, []string{"GET"}) {
		test.Errorf("GET outside a route reported %q, want it reported once", got)
	}
}

// Each allowance keeps a capital only for what it proves, so each is shown both ways: the function it
// covers stays silent, and the same name without the evidence is reported.
func TestConsistencyRequireIdentifierCasingKeepsACapitalOnlyWithItsEvidence(test *testing.T) {
	test.Parallel()

	cases := []struct {
		name     string
		silent   string
		reported string
	}{
		{
			// A Next page's default export, or a stub written before its markup, returns `null` and is a
			// component all the same.
			"a component returning null",
			"export default function KingdomPageRoute() { return null; }\n",
			"export default function KingdomPageRoute() { return 0; }\n",
		},
		{
			"a page that never returns",
			"declare function redirect(path: string): never;\nexport default function RootPageRoute() { redirect('/today'); }\n",
			"declare function redirect(path: string): void;\nexport default function RootPageRoute() { redirect('/today'); }\n",
		},
		{
			"an async component",
			"export async function ReportPage(): Promise<JSX.Element> { return <main />; }\n",
			"export async function ReportPage(): Promise<string> { return ''; }\n",
		},
		{
			"a class decorator factory",
			"type Constructor = new (...values: unknown[]) => object;\n" +
				"export function Injectable() { return function (constructor: Constructor): void { void constructor; }; }\n",
			"export function Injectable() { return function (value: number): void { void value; }; }\n",
		},
		{
			"a member decorator factory",
			"export function GraphQlField(): PropertyDecorator { return () => undefined; }\n",
			"export function GraphQlField(): (target: object, key: string) => void { return () => undefined; }\n",
		},
		{
			"a parameter decorator factory",
			"export function Inject(): ParameterDecorator { return () => undefined; }\n",
			"export function Inject(): (target: object) => void { return () => undefined; }\n",
		},
		{
			"a standard decorator factory",
			"export function Logged() { return function (value: unknown, context: ClassMethodDecoratorContext) { void value; void context; }; }\n",
			"export function Logged() { return function (value: unknown, context: { name: string }) { void value; void context; }; }\n",
		},
		{
			"a function called with new",
			"export function Point(this: { x: number }) {}\nexport const origin = new (Point as unknown as new () => object)();\n",
			"export function Point(this: { x: number }) {}\nexport const origin = (Point as unknown as () => object)();\n",
		},
		{
			"an old-style constructor",
			"export function Point(this: { x: number }, x: number) { (this as { x: number }).x = x; }\n",
			"export function Point(this: { x: number }, x: number) { const other = { x: 0 }; other.x = x; }\n",
		},
	}
	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			if got := identifierCasingFindings(test, "subject.tsx", testCase.silent); len(got) > 0 {
				test.Errorf("the function with its evidence reported %q, want nothing", got)
			}
			if got := identifierCasingFindings(test, "subject.tsx", testCase.reported); len(got) != 1 {
				test.Errorf("the function without its evidence reported %q, want one finding", got)
			}
		})
	}
}

// The advice names the form the rule would accept, as a value's lowerCamelCase, a type's UpperCamelCase, or a
// kept capital's UpperCamelCase.
func TestConsistencyRequireIdentifierCasingAdvice(test *testing.T) {
	test.Parallel()

	cases := []struct {
		name   string
		source string
		wantId string
		want   string
	}{
		{
			"a value",
			"export function sum(first_value: number) { return first_value; }\n",
			"requireLowerCamelCaseValue",
			`The parameter "first_value" is a value, so spell it in lowerCamelCase: "firstValue".`,
		},
		{
			"a type",
			"export interface Order_Line {}\n",
			"requireUpperCamelCaseType",
			`The interface "Order_Line" is a type, so spell it in UpperCamelCase: "OrderLine".`,
		},
		{
			"a kept capital",
			"export function Order_List() { return <ul />; }\n",
			"requireUpperCamelCaseCapital",
			`The component "Order_List" keeps its capital`,
		},
	}
	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.RunTypedFiles(
				test,
				ConsistencyRequireIdentifierCasing,
				identifierCasingFiles("subject.tsx", testCase.source),
				"subject.tsx",
			)
			rule_testing.ExpectFindings(test, result, testCase.wantId)
			if description := result.Diagnostics[0].Message.Description; !strings.HasPrefix(
				description,
				testCase.want,
			) {
				test.Errorf("the message is %q, want it to open %q", description, testCase.want)
			}
		})
	}
}

// A parameter, a local and a file-local function are renamed at every reference; an export, a method and a
// name opening on an acronym are reported without a fix.
func TestConsistencyRequireIdentifierCasingRenamesWhatTheFileOwns(test *testing.T) {
	test.Parallel()

	renamed := []struct {
		name   string
		source string
		want   string
	}{
		{
			"a parameter",
			"export function sum(first_value: number) { return first_value + 1; }\n",
			"export function sum(firstValue: number) { return firstValue + 1; }\n",
		},
		{
			"a local, its shorthand key kept",
			"export function row() { let unit_price = 1; return { unit_price }; }\n",
			"export function row() { let unitPrice = 1; return { unit_price: unitPrice }; }\n",
		},
		{
			"a file-local function",
			"function format_value(value: number) { return String(value); }\nexport const shown = format_value(1);\n",
			"function formatValue(value: number) { return String(value); }\nexport const shown = formatValue(1);\n",
		},
	}
	for _, testCase := range renamed {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.RunTypedFiles(
				test,
				ConsistencyRequireIdentifierCasing,
				identifierCasingFiles("subject.ts", testCase.source),
				"subject.ts",
			)
			rule_testing.ExpectFixedSource(test, result, testCase.want)
		})
	}

	unfixed := []struct {
		name   string
		source string
	}{
		{"an exported function", "export function format_value(value: number) { return String(value); }\n"},
		{"a method", "export class Book { do_work(): void {} }\n"},
		{"a name opening on an acronym", "export function read(URLPath: string) { return URLPath; }\n"},
	}
	for _, testCase := range unfixed {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.RunTypedFiles(
				test,
				ConsistencyRequireIdentifierCasing,
				identifierCasingFiles("subject.ts", testCase.source),
				"subject.ts",
			)
			if len(result.Diagnostics) != 1 {
				test.Fatalf("reported %d findings, want 1: %v", len(result.Diagnostics), result.MessageIds())
			}
			if len(result.Diagnostics[0].Fixes) > 0 {
				test.Errorf("proposed a fix, and its references can reach past this file")
			}
		})
	}
}
