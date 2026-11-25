async function make(): Promise<void> {}
const narrow = [make] as const;
const wide: readonly [() => Promise<number | void>] = narrow;
void wide;
