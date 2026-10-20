type Cell = { value?: string } | { value?: number };
function clear(cell: Cell): void { delete cell.value; }
const counter: { value?: number } = { value: 1 };
clear(counter);
console.log(`${counter.value ?? 0}`);
export {};
