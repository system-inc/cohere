type Holder = { items: string[] } | { items: number[] };
function corrupt(holder: Holder): void { [...holder.items] = ['text']; }
function probe(): void {
const counter: { items: number[] } = { items: [1] };
corrupt(counter);
console.log((counter.items[0] ?? 0).toFixed(1));
}
probe();
export {};
