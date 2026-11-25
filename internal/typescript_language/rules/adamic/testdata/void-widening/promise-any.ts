const make: () => void = () => 'x';
async function run(): Promise<void> {
const narrow: Promise<void> = Promise.resolve(make());
const wide: Promise<any> = narrow;
const next = await wide;
console.log(next.toFixed(1));
}
void run();
