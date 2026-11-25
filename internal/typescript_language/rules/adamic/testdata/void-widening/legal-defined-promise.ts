async function run(): Promise<void> {
 const narrow: Promise<number> = Promise.resolve(42);
 const wide: Promise<number | void> = narrow;
 const value = await wide;
 if (value) console.log(value.toFixed(1));
}
void run();
