/// <reference path="./phantom.d.ts" />
import { Value } from "./value.js";
export const defined = new Value().present;
class Base { readonly value: number = 1; }
class Inherited extends Base { declare readonly value: number; }
class Assigned { declare readonly value: number; constructor() { this.value = 1; } }
new Inherited().value.toFixed();
new Assigned().value.toFixed();
let narrowed: string | number = 1;
if (typeof narrowed === "number") { narrowed = 2; }
const callback: () => void = () => 1;
callback();
