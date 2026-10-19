const make: () => void = () => 'x';
const wider: () => boolean | void = make;
const next = wider();
if (next) { if (next !== true) throw new Error('expected true'); }
