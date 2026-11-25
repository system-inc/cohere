type Cell = { value: string | undefined } | { value: number | undefined };
function corrupt(cell: Cell): void { cell.value ??= 'text'; }
function probe(): void {
const counter: { value: number | undefined } = { value: undefined };
corrupt(counter);
console.log((counter.value ?? 0).toFixed(1));
}
probe();
export {};
