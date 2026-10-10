function make<T extends { readonly value: number }>(): { readonly value: T['value'] } {
 const source = { value: 2 };
 return { ...source };
}
const one: 1 = make<{ readonly value: 1 }>().value;
const names = { 1: 'one' } as const;
const name: 'one' = names[one];
console.log(name.toUpperCase());
export {};
