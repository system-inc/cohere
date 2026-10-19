function make(): void { return void 'x'; }
const wider: () => any = make;
wider();
