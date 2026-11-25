package arrays

// Held is a Block declared apart from every body that reads its length.
var Held Block

// Measured is a Counted, whose length Length folds.
var Measured Counted

// Length is Measured's length, a constant on every platform, 97 on the host's and 13 on windows.
const Length = len(Measured)
