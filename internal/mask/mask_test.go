package mask

import "testing"

func TestEmailMasksMiddle(t *testing.T) {
	if got := Email("admin@qq.com"); got != "adm******com" {
		t.Fatal(got)
	}
}

func TestEmailShort(t *testing.T) {
	if got := Email("a@b.co"); got != "a****o" {
		t.Fatal(got)
	}
}

func TestEmailEmpty(t *testing.T) {
	if Email("  ") != "" {
		t.Fatal("expected empty")
	}
}
