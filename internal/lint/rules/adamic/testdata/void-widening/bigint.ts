const make: () => void = () => 'x';
const wider: () => bigint | void = make;
const next = wider();
if (next) { if (typeof next !== 'bigint') throw new Error('expected bigint'); }
