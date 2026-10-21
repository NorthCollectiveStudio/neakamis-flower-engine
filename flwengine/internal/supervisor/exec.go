package supervisor

import "strings"

// isUnsupportedStart reports whether the error of a failed start is one of
// the "this file cannot be executed here" errors.
//
// The strings come from windows: the errors of CreateProcess when the file
// is blocked (the mark of the web, a cloud placeholder) or has the wrong
// format.
func isUnsupportedStart(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "the request is not supported") ||
		strings.Contains(s, "access is denied") ||
		strings.Contains(s, "is not a valid win32") ||
		strings.Contains(s, "bad exe format") ||
		strings.Contains(s, "exec format error") ||
		strings.Contains(s, "wrong architecture")
}
