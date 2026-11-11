// Package platform declares Handle its own way in each build, and Get, which every build declares alike.
package platform

// Get is a new Handle.
func Get() Handle { return Handle{} }
