import { Box } from './module.js';
declare module './module.js' { interface Box { missing(): number; } }
console.log(String(new Box().missing()));
