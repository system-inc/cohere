// Package values names any for api's OwnLogf, which spells it by this name so that consistency-no-any's presence,
// which reads its own package's spellings of any, names nothing OwnUse uses, and only OwnLogf's fact can take OwnUse
// out.
package values

// Value is any value.
type Value = any
