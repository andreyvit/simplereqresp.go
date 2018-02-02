package simplereqresp

import (
	"bytes"
	"errors"
)

var (
	ErrPartialResponse = errors.New("partial response")
)

var paraSep = []byte("\n\n")
var altParaSep = []byte("\n\r\n")

// ScanFullParagraphs is a split function for bufio.Scanner (and, thus, Client/Server)
// that returns each chunk of text delimited by two consecutive end-of-line markers,
// stripped of the terminator. The returned chunk may be empty.
//
// The end-of-line marker is one optional carriage return followed
// by one mandatory newline. In regular expression notation, it is `\r?\n`.
//
// Unlike bufio.ScanLines, ScanFullLines will return an error if the last chunk
// does not end with two consecutive end-of-line markers.
func ScanFullParagraphs(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}

	if i := bytes.Index(data, paraSep); i >= 0 {
		return i + len(paraSep), dropCR(data[0:i]), nil
	}
	if i := bytes.Index(data, altParaSep); i >= 0 {
		return i + len(altParaSep), dropCR(data[0:i]), nil
	}

	if atEOF {
		return len(data), nil, ErrPartialResponse
	}

	// Request more data.
	return 0, nil, nil
}

// ScanFullLines is a split function for bufio.Scanner (and, thus, Client/Server)
// that returns each line of text, stripped of any trailing end-of-line marker.
// The returned line may be empty.
//
// The end-of-line marker is one optional carriage return followed
// by one mandatory newline. In regular expression notation, it is `\r?\n`.
//
// Unlike bufio.ScanLines, ScanFullLines will return an error if the last line
// does not end with an end-of-line marker.
func ScanFullLines(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}

	if i := bytes.IndexByte(data, '\n'); i >= 0 {
		return i + 1, dropCR(data[0:i]), nil
	}

	if atEOF {
		return len(data), nil, ErrPartialResponse
	}

	// Request more data.
	return 0, nil, nil
}

// dropCR drops a terminal \r from the data.
func dropCR(data []byte) []byte {
	if len(data) > 0 && data[len(data)-1] == '\r' {
		return data[0 : len(data)-1]
	}
	return data
}
