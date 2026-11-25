async function make(): Promise<void> {}
const pending = make();
const wide: Promise<number | void> = pending;
void wide;
