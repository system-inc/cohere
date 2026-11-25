type Cell = { value: string } | { value: number };
function corrupt(cell: Cell, key: 'value'): void { cell[key] = 'text'; }
function probe(): void {
const counter: { value: number } = { value: 1 };
corrupt(counter, 'value');
console.log(counter.value.toFixed(1));
}
probe();
export {};
