const make: () => void = () => 'x';
const wider: () => (() => number) | void = make;
const next = wider();
if (next) { next(); }
