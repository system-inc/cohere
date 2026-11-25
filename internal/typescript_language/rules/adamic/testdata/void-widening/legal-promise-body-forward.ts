async function source(): Promise<void> {}
async function make(): Promise<void> { return source(); }
const wide: Promise<number | void> = make();
void wide;
