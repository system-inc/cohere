function make<T extends { readonly value: number }>(source: T): { get(): T['value'] } {
 return { get() { if (source.value === 0) { return source.value; } return 2; } };
}
const one: 1 = make({ value: 1 } as const).get();
const names = { 1: 'one' } as const;
const name: 'one' = names[one];
console.log(name.toUpperCase());
export {};
