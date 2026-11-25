function make<T extends { readonly value: number }>(): T['value'] {
    const value: T['value'] = 2;
    return value;
}
const names = { 1: 'one' } as const;
const one: 1 = make<{ readonly value: 1 }>();
const name: 'one' = names[one];
console.log(name.toUpperCase());
export {};
