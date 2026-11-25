function make<T extends { readonly value: number }>(source: T): { readonly value: T['value'] } {
 return { ...source, value: 2 };
}
const one: 1 = make({ value: 1 } as const).value;
const names = { 1: 'one' } as const;
const name: 'one' = names[one];
console.log(name.toUpperCase());
export {};
