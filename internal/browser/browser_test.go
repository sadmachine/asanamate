package browser

import "testing"

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
