type Cell = { inner: { value: string } } | { inner: { value: number } };
function corrupt(cell: Cell): void { cell.inner.value = 'text'; }
function probe(): void {
const counter: { inner: { value: number } } = { inner: { value: 1 } };
corrupt(counter);
console.log(counter.inner.value.toFixed(1));
}
probe();
export {};
