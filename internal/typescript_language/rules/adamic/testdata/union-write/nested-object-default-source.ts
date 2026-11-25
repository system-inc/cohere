type Cell = { value: number } | { value: number | string };
function corrupt(cell: Cell): void { ({ a: { text: cell.value = 1 } } = { a: { text: 'text' } }); }
function probe(): void {
const counter: { value: number } = { value: 1 };
corrupt(counter);
console.log(counter.value.toFixed(1));
}
probe();
export {};
