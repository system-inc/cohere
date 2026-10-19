async function run(): Promise<void> { const narrow: Promise<void> = Promise.resolve(); const wide: Promise<unknown> = narrow; const value = await wide; if (typeof value === "number") console.log(value.toFixed(1)); }
void run();
