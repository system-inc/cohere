type Cells = string[] | number[];
function corrupt(cells: Cells): void { cells[0] = 'text'; }
function probe(): void {
const counter: number[] = [1];
corrupt(counter);
console.log((counter[0] ?? 0).toFixed(1));
}
probe();
export {};
