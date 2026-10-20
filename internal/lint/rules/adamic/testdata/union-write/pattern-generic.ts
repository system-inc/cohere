type Cell = { value: string } | { value: number };
function corrupt<T extends Cell>(cell: T): void { [cell.value] = ['text']; }
function probe(): void {
const counter: { value: number } = { value: 1 };
corrupt(counter);
console.log(counter.value.toFixed(1));
}
probe();
export {};
