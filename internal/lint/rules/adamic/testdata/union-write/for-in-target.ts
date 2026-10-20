type Cell = { value: string } | { value: number };
function corrupt(cell: Cell): void { for (cell.value in { text: 1 }) { console.log('wrote'); } }
function probe(): void {
const counter: { value: number } = { value: 1 };
corrupt(counter);
console.log(counter.value.toFixed(1));
}
probe();
export {};
