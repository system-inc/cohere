type Holder = { items: number[] } | { items: (number | string)[] };
function corrupt(holder: Holder, source: [number, number] | [string, string]): void { let first: unknown; [first, ...holder.items] = source; }
function probe(): void {
const counter: { items: number[] } = { items: [1] };
corrupt(counter, ['a', 'text']);
console.log((counter.items[0] ?? 0).toFixed(1));
}
probe();
export {};
