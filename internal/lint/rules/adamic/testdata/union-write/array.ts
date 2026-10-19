type Cells = string[] | number[];
function corrupt(cells: Cells, index: number): void { cells[index] = 'text'; }
function probe(): void {
const counter: number[] = [1];
corrupt(counter, 0);
console.log((counter[0] ?? 0).toFixed(1));
}
probe();
export {};
