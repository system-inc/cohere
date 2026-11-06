// want `^Package name "__utils" is not allowed\. Use "__utilities" instead, `

// A package clause loses every leading underscore before it is judged, not only the first, so __utils is banned.
// A folder can't start with an underscore and still load, so only a clause reaches this.
package __utils

// Clause is something the package holds.
const Clause = 1
