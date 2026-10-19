const hidden: () => void = () => 'x';
async function make(): Promise<void> {}
let narrow = make();
narrow = Promise.resolve(hidden());
const wide: Promise<number | void> = narrow;
async function run(): Promise<void> { const next = await wide; if (next) console.log(next.toFixed(1)); }
void run();
