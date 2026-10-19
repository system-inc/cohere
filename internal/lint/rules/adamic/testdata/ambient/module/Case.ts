import { Value } from "./value.js";
declare module "./value.js" { interface Value { missing(): number } }
new Value().missing().toFixed();
