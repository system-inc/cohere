// want `^Folder name "util" is not allowed\. Use "utilities" instead\.$`

// Every folder of the path is judged, not only the package's own, so a util folder above it is banned.
package inner

// Inner is something the package holds.
const Inner = 1
