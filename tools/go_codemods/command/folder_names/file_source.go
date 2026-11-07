package main

import "os"

// fileSource is a file's contents, read once per file.
type fileSource map[string][]byte

func (sources fileSource) read(filename string) ([]byte, error) {
	if contents, found := sources[filename]; found {
		return contents, nil
	}
	contents, readError := os.ReadFile(filename)
	if readError != nil {
		return nil, readError
	}
	sources[filename] = contents
	return contents, nil
}
