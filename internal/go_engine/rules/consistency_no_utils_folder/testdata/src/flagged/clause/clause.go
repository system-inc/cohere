// want `^Package name "utils" is not allowed\. Use "utilities" instead, `

// The folder is fine and the package clause is not, so the clause is reported.
package utils

// Clause is something the package holds.
const Clause = 1
