type Cell = { value: string } | { value: number };
function corrupt(cell: Cell): void { const source = { ignored: 1, text: 'text' }; ({ text: cell.value } = source); }
function probe(): void {
const counter: { value: number } = { value: 1 };
corrupt(counter);
console.log(counter.value.toFixed(1));
}
probe();
export {};
