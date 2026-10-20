type Cell = { value: string } | { value: number };
function corrupt(cell: Cell, source: { value: string } | { other: number }): void { Object.assign(cell, source); }
function probe(): void {
const counter: { value: number } = { value: 1 };
corrupt(counter, { value: 'text' });
console.log(counter.value.toFixed(1));
}
probe();
export {};
