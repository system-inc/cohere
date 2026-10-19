async function source(): Promise<void> {}
async function forward(): Promise<void> { return source(); }
void forward();
