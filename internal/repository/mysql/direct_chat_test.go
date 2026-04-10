package mysql

import "testing"

func TestNormalizeUserPair(t *testing.T) {
	a, b := normalizeUserPair(10, 3)
	if a != 3 || b != 10 {
		t.Fatalf("got %d,%d", a, b)
	}
	x, y := normalizeUserPair(1, 1)
	if x != 1 || y != 1 {
		t.Fatalf("equal ids: %d,%d", x, y)
	}
}
