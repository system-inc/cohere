type TestFn = (t: unknown) => void | Promise<void>;
function it(name: string, fn: TestFn): void { fn(undefined); }
it('x', function(): void {});
