function make<T extends { readonly value: number }>(): { put(value: number): T['value'] } {
 return { put(value: T['value']) { return value; } };
}
const one: 1 = make<{ readonly value: 1 }>().put(2);
const names = { 1: 'one' } as const;
const name: 'one' = names[one];
console.log(name.toUpperCase());
export {};
