type Cell = { value: string } | { value: number };
function corrupt(cell: Cell): void { Object.assign(cell, { value: 1 }); }
function probe(): void {
const counter: { value: string } = { value: "text" };
corrupt(counter);
console.log(counter.value.toUpperCase());
}
probe();
export {};
