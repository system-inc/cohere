const make: () => void = () => 'x';
const consume = (wider: () => number | void): void => {
const next = wider();
if (next) console.log(next.toFixed(1));
};
const narrow: (fn: () => void) => void = consume;
narrow(make);
