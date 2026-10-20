type Holder = { bag: { text: string } } | { bag: { count: number } };
function corrupt(holder: Holder): void { let first = 0; ({ count: first, ...holder.bag } = { count: 1, text: 'ahra' }); console.log(first.toFixed(1)); }
function probe(): void {
const counter: { bag: { count: number } } = { bag: { count: 1 } };
corrupt(counter);
console.log(counter.bag.count.toFixed(1));
}
probe();
export {};
