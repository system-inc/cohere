type Cell = { a: string; b: string } | { a: string; b: number };
function corrupt(cell: Cell, key: 'a' | 'b'): void { cell[key] = 'text'; }
function probe(): void {
const counter: { a: string; b: number } = { a: 'text', b: 1 };
corrupt(counter, 'b');
console.log(counter.b.toFixed(1));
}
probe();
export {};
