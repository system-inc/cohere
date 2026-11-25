import { format as renamed } from './helpersOne';

// Same import as importOne.ts under a different local name: should match importsA.
export function importsSameA(value: number): number {
    return renamed(value) * 3;
}
