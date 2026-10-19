type Record1 = { label?: string } | { label?: number };
function corrupt(record: Record1): void { record.label = 'text'; }
function probe(): void {
const item: { label?: number } = { label: 1 };
corrupt(item);
console.log((item.label ?? 0).toFixed(1));
}
probe();
export {};
