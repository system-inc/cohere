package differences

import "fmt"

// The file still uses fmt once every fix of it is applied, so the fixed file is judged by the fixes alone.
var _ = fmt.Sprint

func indexed(first string, second string, channel chan string, pointer *string) {
	// The original writes `first + second[0]`, which indexes second alone and adds a byte to a string.
	_ = fmt.Sprintf("%s", first+second)[0] // port moves: the argument is already a string, there's no need to use fmt.Sprintf
	// The original writes `<-channel[1:]`, which slices the channel.
	_ = fmt.Sprintf("%s", <-channel)[1:] // port moves: the argument is already a string, there's no need to use fmt.Sprintf
	// The original writes `*pointer[0]`, which indexes the pointer.
	_ = fmt.Sprintf("%s", *pointer)[0] // port moves: the argument is already a string, there's no need to use fmt.Sprintf
}
