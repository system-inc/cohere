// want `^Package name "Utils" is not allowed\. Use "utilities" instead, `

// A package clause is judged as a folder is, lowercased, so Utils is banned.
package Utils

// Clause is something the package holds.
const Clause = 1
