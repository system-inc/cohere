async function run(): Promise<void> { const pending: Promise<void> = Promise.resolve(); await pending; }
void run();
