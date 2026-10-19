async function make(): Promise<void> {}
const narrow = [make];
const wide: readonly (() => Promise<number | void>)[] = narrow;
void wide;
