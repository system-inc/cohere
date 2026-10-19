function discard(fn: () => void): void { fn(); }
discard(() => 42);
