function run(): void {
 class Promise<T> {}
 const narrow: Promise<void> = new Promise<void>();
 const wide: Promise<number | void> = narrow;
 console.log(String(wide instanceof Promise));
}
run();
