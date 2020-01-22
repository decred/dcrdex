package pg

import (
	"testing"
)

func TestParseUnit(t *testing.T) {
	tests := []struct {
		in   string
		mult float64
		base string
		fail bool
	}{
		{"1", 1.0, "", false},                        // no base unit
		{" ", 1.0, "", false},                        // white space
		{"8kB", 8.0, "kB", false},                    // basic
		{"8 kB ", 8.0, "kB", false},                  // spaces discarded
		{"kB", 1.0, "kB", false},                     // no numeric part
		{".8kB", 0.8, "kB", false},                   // decimal w/ no int
		{"122B", 122.0, "B", false},                  // different base unit
		{"-400MB", -400.0, "MB", false},              // negative
		{".kB", 0.0, "", true},                       // invalid numeric part
		{"1.1.1kB", 0.0, "", true},                   // invalid numeric part
		{"8kB63", 8.0, "kB63", false},                // number in base unit
		{"1.21 GW", 1.21, "GW", false},               // strip space between number and base unit
		{"J/s", 1.0, "J/s", false},                   // complex base unit
		{"kg m^2 / s^2", 1.0, "kg m^2 / s^2", false}, // complex base unit
		{"-v", -1.0, "v", false},                     // consider a lone dash prefix as a negation
		{"7 dog years", 7.0, "dog years", false},     // base unit with spaces
		{"", 1.0, "", false},                         // empty string
	}

	for i := range tests {
		ti := &tests[i]
		mult, base, err := parseUnit(ti.in)
		if err != nil && !ti.fail {
			t.Errorf("parseUnit(%s) failed: %v", ti.in, err)
		} else if err == nil && ti.fail {
			t.Errorf("parseUnit(%s) was supposed to fail.", ti.in)
		}
		if mult != ti.mult {
			t.Errorf("parseUnit(%s) returned mult %f, expected %f", ti.in, mult, ti.mult)
		}
		if base != ti.base {
			t.Errorf("parseUnit(%s) returned base %s, expected %s", ti.in, base, ti.base)
		}
		//t.Logf("multiple=%f, base=%s", mult, base)
	}

}
