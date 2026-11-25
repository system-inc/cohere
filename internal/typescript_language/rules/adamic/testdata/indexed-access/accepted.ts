function make<T extends { readonly value: number }>(source: T): T['value'] {
    const value: T['value'] = source.value;
    return value;
}
const names = { 1: 'one' } as const;
const one: 1 = make({ value: 1 } as const);
const name: 'one' = names[one];
console.log(name.toUpperCase());
export {};
