package mask

import "strings"

// Email keeps a few characters at each end and replaces the middle with stars.
func Email(email string) string {
	chars := []rune(strings.TrimSpace(email))
	n := len(chars)
	if n == 0 {
		return ""
	}
	head, tail := 3, 3
	if n <= 8 {
		head, tail = 1, 1
	}
	if head+tail >= n {
		return strings.Repeat("*", n)
	}
	return string(chars[:head]) + strings.Repeat("*", n-head-tail) + string(chars[n-tail:])
}
