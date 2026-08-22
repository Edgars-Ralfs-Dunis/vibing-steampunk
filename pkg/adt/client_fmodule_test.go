package adt

import "testing"

// A function module carries no package of its own; the allowlist check must
// resolve its function group. Without this, editing any FM failed with
// "resolving package for ...: package metadata not found".
func TestNormalizeObjectURLStripsFmodules(t *testing.T) {
	got := normalizeObjectURLForPackageCheck("/sap/bc/adt/functions/groups/ZPCC_API/fmodules/Z_PCC_RULE_APPLY")
	want := "/sap/bc/adt/functions/groups/ZPCC_API"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestNormalizeObjectURLLeavesGroupAlone(t *testing.T) {
	want := "/sap/bc/adt/functions/groups/ZPCC_API"
	if got := normalizeObjectURLForPackageCheck(want); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestObjectNameFromFmoduleURLIsTheGroup(t *testing.T) {
	name, err := objectNameFromURL("/sap/bc/adt/functions/groups/ZPCC_API/fmodules/Z_PCC_RULE_APPLY")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "ZPCC_API" {
		t.Errorf("got %q, want ZPCC_API", name)
	}
}
