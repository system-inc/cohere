type Cell = { value: 1 } | { value: 2 };
function corrupt(cell: Cell): void { --cell.value; }
function probe(): void {
const counter: { value: 1 } = { value: 1 };
corrupt(counter);
const names = { 1: 'one', 2: 'two' };
console.log(names[counter.value].toUpperCase());
}
probe();
export {};
