const object = { make(): void {} };
const wider: () => any = object.make;
wider();
