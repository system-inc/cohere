const make: () => void = () => 'x';
const wider: () => symbol | void = make;
const next = wider();
if (next) { if (typeof next !== 'symbol') throw new Error('expected symbol'); }
