package strutils

import "unicode/utf8"

func LimitString(s string, maxLength int) string {
	max := 0
	if maxLength > 0 {
		max = maxLength
	}
	// Check if the string is already within the desired length
	if utf8.RuneCountInString(s) <= max {
		return s
	}

	// Truncate the string to the desired length
	runes := []rune(s)
	truncatedRunes := runes[:max]
	return string(truncatedRunes)
}
