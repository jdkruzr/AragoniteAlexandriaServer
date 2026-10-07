package wire

import "testing"

func TestULIDsAreWellFormedAndOrdered(t *testing.T) {
	a := NewULID()
	if !IsULID(a) || a[0] > '7' {
		t.Fatalf("bad ULID %q", a)
	}
	for _, bad := range []string{"", "0123456789ABCDEFGHJKMNPQR", "0123456789ABCDEFGHJKMNPQRI", "0123456789abcdefghjkmnpqrs"} {
		if IsULID(bad) {
			t.Fatalf("accepted %q", bad)
		}
	}
}
