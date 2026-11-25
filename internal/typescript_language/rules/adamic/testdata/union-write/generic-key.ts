type Cell = { value: string } | { value: number };
function corrupt<K extends 'value'>(cell: Cell, key: K): void { cell[key] = 'text'; }
function probe(): void {
const counter: { value: number } = { value: 1 };
corrupt(counter, 'value');
console.log(counter.value.toFixed(1));
}
probe();
export {};
