function make<T extends { readonly value: number }>(): { get(): T['value'] } {
 return { get() { return 2; } };
}
const one: 1 = make<{ readonly value: 1 }>().get();
const names = { 1: 'one' } as const;
const name: 'one' = names[one];
console.log(name.toUpperCase());
export {};
