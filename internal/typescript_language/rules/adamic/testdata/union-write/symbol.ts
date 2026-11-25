const key = Symbol('value');
type Cell = { [key]: string } | { [key]: number };
function corrupt(cell: Cell): void { cell[key] = 'text'; }
function probe(): void {
const counter: { [key]: number } = { [key]: 1 };
corrupt(counter);
console.log(counter[key].toFixed(1));
}
probe();
export {};
