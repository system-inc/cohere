// any: tsc keeps P, so the return is TS2339
enum Kind { A, B }
enum Other { X, Y }
type P = { kind: Kind.A; value: string };
type Q = { kind: any; value: number };
function f(v: P | Q): string {
  if (v.kind === Other.X) {
    return v.value.toFixed(1);
  }
  return 'other';
}
console.log(f({ kind: Kind.A, value: 'text1' }));
export {};
