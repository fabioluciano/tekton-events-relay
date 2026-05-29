// Package stringutil provides common string manipulation utilities.
package stringutil

// Truncate returns s truncated to at most n characters.
// If s is shorter than n, it is returned unchanged.
func Truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// TruncateWithEllipsis returns s truncated to at most n characters with "..." appended.
// If s is shorter than n, it is returned unchanged.
// If truncation occurs, the result will be n characters total (including "...").
func TruncateWithEllipsis(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n < 3 {
		return s[:n]
	}
	return s[:n-3] + "..."
}
