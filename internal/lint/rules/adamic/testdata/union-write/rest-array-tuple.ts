type Holder = { items: string[] } | { items: number[] };
function corrupt(holder: Holder): void { const source: [number, string] = [1, 'text']; let first = 0; [first, ...holder.items] = source; console.log(first.toFixed(1)); }
function probe(): void {
const counter: { items: number[] } = { items: [1] };
corrupt(counter);
console.log((counter.items[0] ?? 0).toFixed(1));
}
probe();
export {};
