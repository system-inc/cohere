// Package api declares Old in a file every build compiles, with no doc comment of its own. The deprecated facts
// analyzer carries a deprecation paragraph above an empty group to the next declaration it meets, in that file or the
// next, so Old is deprecated in exactly the builds that compile the platform file holding the paragraph.
package api

func Old() {
	// Does nothing: a call of it is a use of a deprecated function, or not, and nothing else.
}
