type Holder = { bag: { text: string } } | { bag: { count: number } };
function corrupt(holder: Holder): void { ({ ...holder.bag } = { text: 'ahra' }); }
function probe(): void {
const counter: { bag: { count: number } } = { bag: { count: 1 } };
corrupt(counter);
console.log(counter.bag.count.toFixed(1));
}
probe();
export {};
