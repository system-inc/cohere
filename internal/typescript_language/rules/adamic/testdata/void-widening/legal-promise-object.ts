async function make(): Promise<void> {}
const narrow = { fn: make };
const wide: { readonly fn: () => Promise<number | void> } = narrow;
void wide;
