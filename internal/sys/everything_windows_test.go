//go:build windows

package sys

import "testing"

func TestFirstPathLine(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"single", `C:\foo\bar.pdf`, `C:\foo\bar.pdf`},
		{"multiline", "C:\\foo\\bar.pdf\r\nC:\\foo\\baz.pdf\r\n", `C:\foo\bar.pdf`},
		{"blank lines", "\r\n\r\nC:\\foo\\bar.pdf", `C:\foo\bar.pdf`},
		{"quoted", `"C:\foo\bar.pdf"`, `C:\foo\bar.pdf`},
		{"surrounding spaces", "  C:\\foo\\bar.pdf  \r\n", `C:\foo\bar.pdf`},
		{"empty", " \r\n ", ""},
	}
	for _, tc := range cases {
		if got := firstPathLine(tc.in); got != tc.want {
			t.Fatalf("%s: firstPathLine(%q) = %q, want %q", tc.name, tc.in, got, tc.want)
		}
	}
}

func TestGetSelectedFromEverythingWithoutWindow(t *testing.T) {
	if got := getSelectedFromEverything(0); got != "" {
		t.Fatalf("getSelectedFromEverything(0) = %q, want empty", got)
	}
}
