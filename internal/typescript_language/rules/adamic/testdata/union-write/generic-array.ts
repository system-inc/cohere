type Cells = string[] | number[];
function corrupt<T extends Cells>(cells: T, index: number): void { cells[index] = 'text'; }
function probe(): void {
const counter: number[] = [1];
corrupt(counter, 0);
console.log((counter[0] ?? 0).toFixed(1));
}
probe();
export {};
