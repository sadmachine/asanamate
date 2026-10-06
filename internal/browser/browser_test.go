package browser

import (
	"strings"
	"testing"
)

func TestValidateRejectsUnsafeURLs(t *testing.T) {
	for _, u := range []string{"file:///etc/passwd", "-a Calculator", "javascript:alert(1)", ""} {
		if err := validate(u); err == nil {
			t.Errorf("validate(%q) accepted", u)
		}
	}
	if err := validate("https://app.asana.com/0/1/2"); err != nil {
		t.Fatal(err)
	}
}

func TestOpenFailureKeepsURL(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	const u = "https://app.asana.com/0/1/2"
	if err := Open(u); err == nil || !strings.Contains(err.Error(), u) {
		t.Fatalf("err = %v", err)
	}
}
