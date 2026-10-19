const make: () => void = () => 'x';
async function run(): Promise<void> {
const narrow: Promise<void> = Promise.resolve(make());
const wide: Promise<number | void> = narrow;
const next = await wide;
if (next) console.log(next.toFixed(1));
}
void run();
