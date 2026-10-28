package billing

import "testing"

func TestCumulativeMaximumAndFractionalRate(t *testing.T) {
	n, _, _, e := Delta(100, 0, 0, 80, "max", 1000)
	if e != nil || n != 0 {
		t.Fatal(n, e)
	}
	n, _, _, e = Delta(100, 80, 0, 50, "max", 1000)
	if e != nil || n != 30 {
		t.Fatal(n, e)
	}
	n, u, d, e := Delta(1, 0, 0, 1, "both", 500)
	if e != nil || n != 1 || u+d != n {
		t.Fatal(n, u, d, e)
	}
	if _, e = Charge(9000000000000000, 9000000000000000, "both", 100000); e != nil {
		t.Fatal(e)
	}
	if _, e = Charge(0, 0, "shell", 1000); e == nil {
		t.Fatal("invalid mode")
	}
}
