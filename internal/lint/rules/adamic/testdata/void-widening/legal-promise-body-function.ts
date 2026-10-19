async function make(): Promise<void> {}
const wider: () => Promise<number | void> = make;
void wider();
