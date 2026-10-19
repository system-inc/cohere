const hidden: () => void = () => 'x';
async function make(): Promise<void> { return hidden(); }
async function run(narrow: readonly (() => Promise<void>)[] | readonly [() => Promise<void>]): Promise<void> {
 const wide: readonly (() => Promise<number | void>)[] = narrow;
 for (const fn of wide) {
  const next = await fn();
  if (next) console.log(next.toFixed(1));
 }
}
void run([make]);
