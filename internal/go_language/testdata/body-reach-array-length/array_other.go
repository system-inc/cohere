//go:build !windows

package arrays

// Block is every platform's but windows's, an array, whose length is a constant.
type Block [97]byte

// Counted is every platform's but windows's, an array of 97.
type Counted [97]byte
