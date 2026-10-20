type Holder = { bag: { count: number } } | { bag: { count?: number; text?: string } };
function corrupt(holder: Holder, source: { count: number } | { text: string }): void { ({ ...holder.bag } = source); }
function probe(): void {
const counter: { bag: { count: number } } = { bag: { count: 1 } };
corrupt(counter, { text: 'ahra' });
console.log(counter.bag.count.toFixed(1));
}
probe();
export {};
