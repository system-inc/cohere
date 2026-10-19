const make: () => void = () => 42;
const wider: () => string | void = make;
const next = wider();
if (next) { next.toUpperCase(); }
