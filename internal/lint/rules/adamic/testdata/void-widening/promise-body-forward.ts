const hidden: () => void = () => 'x';
async function make(): Promise<void> { return hidden(); }
const wider: () => Promise<number | void> = make;
async function run(): Promise<void> { const next = await wider(); if (next) console.log(next.toFixed(1)); }
void run();
