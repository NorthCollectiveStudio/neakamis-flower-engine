package supervisor

import "testing"

// The error of a failed start tells whether the file itself cannot be
// executed at its place. On windows that is the mark of the web or a cloud
// placeholder: the copy in the temporary directory is tried then.
func TestIsUnsupportedStart(t *testing.T) {
	cases := []struct {
		msg  string
		want bool
	}{
		{"fork/exec C:\\build\\flwengine.exe: The request is not supported", true},
		{"fork/exec C:\\build\\flwengine.exe: Access is denied.", true},
		{"%1 is not a valid Win32 application.", true},
		{"exec format error", true},
		{"fork/exec C:\\build\\flwengine.exe: %!w(MISSING)", false},
		{"", false},
	}
	for _, c := range cases {
		if got := isUnsupportedStart(errString(c.msg)); got != c.want {
			t.Errorf("isUnsupportedStart(%q) = %v, want %v", c.msg, got, c.want)
		}
	}
	if isUnsupportedStart(nil) {
		t.Error("isUnsupportedStart(nil) must be false")
	}
}

type errString string

func (e errString) Error() string { return string(e) }
