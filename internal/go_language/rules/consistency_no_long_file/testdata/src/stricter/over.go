// Package stricter is run with a limit of 50: this file of 51 lines is flagged at its 51st.
package stricter

const overRawText = `a raw string
whose newlines are lines too`

/*
A block comment, whose lines count as well.
*/
const overValue10 = 10
const overValue11 = 11
const overValue12 = 12
const overValue13 = 13
const overValue14 = 14
const overValue15 = 15
const overValue16 = 16
const overValue17 = 17
const overValue18 = 18
const overValue19 = 19
const overValue20 = 20
const overValue21 = 21
const overValue22 = 22
const overValue23 = 23
const overValue24 = 24
const overValue25 = 25
const overValue26 = 26
const overValue27 = 27
const overValue28 = 28
const overValue29 = 29
const overValue30 = 30
const overValue31 = 31
const overValue32 = 32
const overValue33 = 33
const overValue34 = 34
const overValue35 = 35
const overValue36 = 36
const overValue37 = 37
const overValue38 = 38
const overValue39 = 39
const overValue40 = 40
const overValue41 = 41
const overValue42 = 42
const overValue43 = 43
const overValue44 = 44
const overValue45 = 45
const overValue46 = 46
const overValue47 = 47
const overValue48 = 48
const overValue49 = 49
const overValue50 = 50
const valueLast = 0 // want "This file is 51 lines, over the 50 a file may hold"
