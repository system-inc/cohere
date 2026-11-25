type Cell = { value: 1 } | { value: 1 | 2 };
function corrupt(cell: Cell): void { cell.value += 1; }
function probe(): void {
const counter: { value: 1 } = { value: 1 };
corrupt(counter);
const names = { 1: 'one' };
console.log(names[counter.value].toUpperCase());
}
probe();
export {};
