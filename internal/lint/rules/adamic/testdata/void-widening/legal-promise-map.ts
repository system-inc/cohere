async function make(): Promise<void> {}
const narrow = new Map<string, typeof make>([['fn', make]]);
const wide: ReadonlyMap<string, () => Promise<number | void>> = narrow;
void wide;
