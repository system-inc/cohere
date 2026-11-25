const make: () => void = () => 'x';
const wider: () => object | void = make;
const next = wider();
if (next) { if (typeof next !== 'object' && typeof next !== 'function') throw new Error('expected object'); }
