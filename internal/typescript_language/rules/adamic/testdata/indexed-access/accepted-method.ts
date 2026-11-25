function make<T extends { readonly value: number }>(source: T): { get(): T['value'] } {
    return { get() { return source.value; } };
}
const names = { 1: 'one' } as const;
const one: 1 = make({ value: 1 } as const).get();
const name: 'one' = names[one];
console.log(name.toUpperCase());
export {};
