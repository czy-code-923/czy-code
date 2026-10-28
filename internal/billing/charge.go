// Package billing keeps quota arithmetic integer and cumulative. In particular,
// max mode charges the larger period total, never the sum of per-sample maxima.
package billing

import (
	"errors"
	"math"
	"math/big"
)

func ValidMode(mode string) bool {
	return mode == "both" || mode == "uplink" || mode == "downlink" || mode == "max"
}
func Charge(up, down int64, mode string, milli int) (int64, error) {
	if up < 0 || down < 0 || !ValidMode(mode) || milli < 1 || milli > 100000 {
		return 0, errors.New("invalid quota accounting parameters")
	}
	a, b := big.NewInt(up), big.NewInt(down)
	switch mode {
	case "both":
		a.Add(a, b)
	case "downlink":
		a = b
	case "max":
		if down > up {
			a = b
		}
	}
	a.Mul(a, big.NewInt(int64(milli)))
	a.Quo(a, big.NewInt(1000))
	if !a.IsInt64() {
		return 0, errors.New("quota counter overflow")
	}
	return a.Int64(), nil
}

// Delta and the billed header split always sum exactly, including fractional rates.
func Delta(up, down, du, dd int64, mode string, milli int) (total, chargedUp, chargedDown int64, err error) {
	if du < 0 || dd < 0 || up > math.MaxInt64-du || down > math.MaxInt64-dd {
		return 0, 0, 0, errors.New("traffic delta overflow")
	}
	before, e := Charge(up, down, mode, milli)
	if e != nil {
		return 0, 0, 0, e
	}
	after, e := Charge(up+du, down+dd, mode, milli)
	if e != nil || after < before {
		return 0, 0, 0, errors.New("invalid quota delta")
	}
	total = after - before
	switch mode {
	case "uplink":
		chargedUp = total
	case "downlink":
		chargedDown = total
	case "max":
		if up+du >= down+dd {
			chargedUp = total
		} else {
			chargedDown = total
		}
	default:
		if du+dd > 0 {
			n := new(big.Int).Mul(big.NewInt(total), big.NewInt(du))
			n.Quo(n, big.NewInt(du+dd))
			chargedUp = n.Int64()
			chargedDown = total - chargedUp
		}
	}
	return
}
