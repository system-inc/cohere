function make(): void { const nested = () => 'x'; void nested; }
const wider: () => any = make;
wider();
