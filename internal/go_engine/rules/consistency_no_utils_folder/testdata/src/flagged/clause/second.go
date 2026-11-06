// want `^Package name "utils" is not allowed\. Use "utilities" instead, `

// A second file with the same clause is reported too, since the finding is once per file.
package utils

// Second is something else the package holds.
const Second = 2
