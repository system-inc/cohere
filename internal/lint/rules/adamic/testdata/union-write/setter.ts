type Cell = { get value(): number; set value(v: number) } | { get value(): number; set value(v: number | string) };
function corrupt(cell: Cell): void { cell.value = 'text'; }
function probe(): void {
let value = 1;
const counter = { get value(): number { return value; }, set value(v: number) { value = v; } };
corrupt(counter);
console.log(counter.value.toFixed(1));
}
probe();
export {};
