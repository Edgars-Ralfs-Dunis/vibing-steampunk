package mcp

import "testing"

// The old prefix heuristic (ZCL_/YCL_/ZIF_/...) silently missed SAP-standard
// classes and Z classes not named ZCL_*, so a bare class name reached SAP and was
// rejected with "You can only set breakpoints in an active, unchanged source".
// Name-based detection is gone; what remains must still build the pool correctly
// and never touch a name that is already one.
func TestClassPoolName(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"ZCL_TEST", "ZCL_TEST======================CP"},
		{"CL_SPFL_PROFILE_PARAMETER", "CL_SPFL_PROFILE_PARAMETER=====CP"},
		{"ZADT_CL_TADIR_MOVE", "ZADT_CL_TADIR_MOVE============CP"},
		// Exactly 30 chars: pads to nothing but must still get the CP suffix.
		// The old code dropped it entirely for len >= 30.
		{"ZCL_AAAAAAAAAAAAAAAAAAAAAAAAAA", "ZCL_AAAAAAAAAAAAAAAAAAAAAAAAAACP"},
	} {
		got := classPoolName(tc.in)
		if got != tc.want {
			t.Errorf("classPoolName(%q) = %q, want %q", tc.in, got, tc.want)
		}
		if len(got) != 32 {
			t.Errorf("classPoolName(%q) length = %d, want 32", tc.in, len(got))
		}
	}
}

func TestIsClassPool(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want bool
	}{
		{"ZCL_TEST======================CP", true},
		{"ZCL_TEST", false},
		{"RSPARAM", false},
		{"LZVSP_RPTU01", false},
	} {
		if got := isClassPool(tc.in); got != tc.want {
			t.Errorf("isClassPool(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}
