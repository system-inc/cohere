// Package corpus holds nothing: no test of cohere reads another repository (Kirk, Nov 25, #5wj23dd).
//
// This file stays for one landing because main's land-job at 69a8e49c judges the landing that removes the corpora,
// and its preflight refuses any cohere tree with no file of this name (pinnedFileIn, #3kpxrdt). That landing removes
// the check, and the landing after it deletes this file.
package corpus
