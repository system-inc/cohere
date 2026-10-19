type Cell = { inner: [string] } | { inner: [number] };
function corrupt(cell: Cell): void { cell.inner[0] = 'text'; }
function probe(): void {
const counter: { inner: [number] } = { inner: [1] };
corrupt(counter);
console.log(counter.inner[0].toFixed(1));
}
probe();
export {};
