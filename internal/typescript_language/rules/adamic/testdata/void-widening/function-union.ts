const make: () => void = () => 'x';
function read(wider: (() => void) | (() => number)): void {
const next = wider();
if (next) console.log(next.toFixed(1));
}
read(make);
