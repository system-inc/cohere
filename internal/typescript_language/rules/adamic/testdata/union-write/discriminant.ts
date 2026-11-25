type Shape = { kind: 'Circle'; radius: number } | { kind: 'Square'; side: number };
function corrupt(shape: Shape): void { shape.kind = 'Square'; }
function area(shape: Shape): string {
 switch (shape.kind) {
 case 'Circle': return shape.radius.toFixed(1);
 case 'Square': return shape.side.toFixed(1);
 }
}
function probe(): void {
const circle: Shape = { kind: 'Circle', radius: 2 };
corrupt(circle);
console.log(area(circle));
}
probe();
export {};
