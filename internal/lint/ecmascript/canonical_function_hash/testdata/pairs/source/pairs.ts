// Expected same.

export function namesA(first: number, second: number): number {
    const total = first * second;
    return total - 1;
}
export function namesB(x: number, y: number): number {
    const product = x * y;
    return product - 1;
}

export function orderA(a: number, b: number): number {
    const left = a * 2;
    const right = b * 3;
    return left - right;
}
export function orderB(a: number, b: number): number {
    const right = b * 3;
    const left = a * 2;
    return left - right;
}

export function commuteA(a: number, b: number): number {
    return (a + b) / 2;
}
export function commuteB(a: number, b: number): number {
    return (b + a) / 2;
}

export function literalA(value: number): number {
    return value * 16;
}
export function literalB(value: number): number {
    return value * 0x10;
}

export function ternaryA(condition: boolean, a: number, b: number): number {
    const result = condition ? a : b;
    return result * 2;
}
export function ternaryB(condition: boolean, a: number, b: number): number {
    let result: number;
    if (condition) {
        result = a;
    } else {
        result = b;
    }
    return result * 2;
}

export function earlyA(value: number, a: number, b: number): number {
    if (value > 0) {
        return a * 2;
    }
    return b - 1;
}
export function earlyB(value: number, a: number, b: number): number {
    if (value > 0) {
        return a * 2;
    } else {
        return b - 1;
    }
}

export function andA(a: number, b: number): number {
    return a && b;
}
export function andB(a: number, b: number): number {
    return a ? b : a;
}

// Expected different.

export function subtractA(a: number, b: number): number {
    return a - b;
}
export function subtractB(a: number, b: number): number {
    return b - a;
}

export function callsA(a: number, b: number): void {
    record(a);
    record(b);
}
export function callsB(a: number, b: number): void {
    record(b);
    record(a);
}

export class Holder {
    sharedValue = 1;
    thisA(): number {
        return this.sharedValue + 1;
    }
}
export function thisB(): number {
    return sharedValue + 1;
}

export function stringsA(a: string, b: string): string {
    return a + b;
}
export function stringsB(a: string, b: string): string {
    return b + a;
}

export function nullishA(a: number | null, b: number): number {
    return a ?? b;
}
export function nullishB(a: number | null, b: number): number {
    return a || b;
}

export function resultsA(read: () => number): number {
    const first = read();
    const second = read();
    return first - second;
}
export function resultsB(read: () => number): number {
    const first = read();
    const second = read();
    return second - first;
}

export function storeA(target: { value: number }): number {
    const before = target.value;
    target.value = 5;
    return before;
}
export function storeB(target: { value: number }): number {
    target.value = 5;
    const after = target.value;
    return after;
}

export function allocationsA(): number | undefined {
    const one: { value?: number } = {};
    const two: { value?: number } = {};
    one.value = 1;
    return two.value;
}
export function allocationsB(): number | undefined {
    const one: { value?: number } = {};
    const two: { value?: number } = {};
    two.value = 1;
    return two.value;
}

export function loopsA(limit: number): number {
    let sum = 0;
    for (let i = 0, j = 0; i < limit; i++, j += 2) {
        sum += i;
    }
    return sum;
}
export function loopsB(limit: number): number {
    let sum = 0;
    for (let i = 0, j = 0; i < limit; i++, j += 2) {
        sum += j;
    }
    return sum;
}

export function literalKindA(): number | string {
    return 16;
}
export function literalKindB(): number | string {
    return '16';
}

export function closureA(a: number, b: number): () => number {
    return () => a * 2;
}
export function closureB(a: number, b: number): () => number {
    return () => b * 2;
}

export function counterA(): number {
    let count = 0;
    const bump = () => {
        count += 1;
    };
    bump();
    return count;
}
export function counterB(): number {
    let count = 0;
    const bump = () => {
        count += 1;
    };
    return count;
}

export function methodsA(input: number): { run(): number } {
    function helper(value: number): number {
        return value + 1;
    }
    return {
        run() {
            return helper(input);
        },
    };
}
export function methodsB(input: number): { run(): number } {
    function helper(value: number): number {
        return value - 1;
    }
    return {
        run() {
            return helper(input);
        },
    };
}

export function defaultsA(value = 1): number {
    return value * 2;
}
export function defaultsB(value = 2): number {
    return value * 2;
}

export class StoreOne {
    constructor(private readonly database: object) {}
}
export class StoreTwo {
    constructor(private readonly client: object) {}
}

// Added in cohere (#jfvhbj7), beside the prototype's pairs. Expected same.

export function copiesA(a: number, b: number): number {
    return (a - b) * 3;
}
export function copiesB(a: number, b: number): number {
    const difference = a - b;
    const tripled = difference * 3;
    return tripled;
}

export function literalSeparated(value: number): number {
    return value * 1_6;
}
export function literalDecimal(value: number): number {
    return value * 16.0;
}

export function defaultsSameA(first = 3): number {
    return first * 5;
}
export function defaultsSameB(second = 3): number {
    return second * 5;
}

export class PropertyOne {
    constructor(private readonly connection: object) {}
}
export class PropertyTwo {
    constructor(private readonly connection: object) {}
}

export function bigintA(a: bigint, b: bigint): bigint {
    return (a * b) - 1n;
}
export function bigintB(a: bigint, b: bigint): bigint {
    return (b * a) - 1n;
}

// Expected different.

export class PropertyPublic {
    constructor(public readonly connection: object) {}
}
export class PropertyString {
    constructor(private readonly connection: string) {}
}

// Refused.

export function defaultsComputed(value = Date.now()): number {
    return value * 2;
}

export async function loadsLater(): Promise<unknown> {
    return import('./helpersOne');
}

export class Initialized {
    count = 1;
    constructor(readonly start: number) {}
}

export function holdsAClass(): object {
    class Inner {}
    return new Inner();
}
