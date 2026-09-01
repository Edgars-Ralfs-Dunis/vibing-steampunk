package embedded

import "testing"

// Every object must carry a Description. WriteSource's create path bails out with
// "Description is required for creating new objects" and returns a NIL error, so a
// missing description here becomes a silent no-op that the installer reports as
// "✓ Deployed". Observed on ITH 2026-08-23: all 8 objects reported deployed, none written.
func TestGetObjects_AllHaveSourceAndDescription(t *testing.T) {
	for _, obj := range GetObjects() {
		if obj.Name == "" {
			t.Fatal("object with empty Name")
		}
		if obj.Source == "" {
			t.Errorf("%s: empty Source", obj.Name)
		}
		if obj.Description == "" {
			t.Errorf("%s: empty Description — create path will silently no-op", obj.Name)
		}
		if obj.Type != "CLAS" && obj.Type != "INTF" && obj.Type != "PROG" {
			t.Errorf("%s: unexpected Type %q", obj.Name, obj.Type)
		}
	}
}

// ZCL_VSP_AMDP_SERVICE references zcl_adt_00_amdp_test=>tt_result and
// =>calculate_squares statically, so the demo class must ship AND be deployed first.
func TestGetObjects_AmdpTestPrecedesAmdpService(t *testing.T) {
	iTest, iSvc := -1, -1
	for i, obj := range GetObjects() {
		switch obj.Name {
		case "ZCL_ADT_00_AMDP_TEST":
			iTest = i
		case "ZCL_VSP_AMDP_SERVICE":
			iSvc = i
		}
	}
	if iTest < 0 {
		t.Fatal("ZCL_ADT_00_AMDP_TEST missing from GetObjects — ZCL_VSP_AMDP_SERVICE cannot compile")
	}
	if iSvc < 0 {
		t.Fatal("ZCL_VSP_AMDP_SERVICE missing from GetObjects")
	}
	if iTest > iSvc {
		t.Errorf("ZCL_ADT_00_AMDP_TEST at %d must precede ZCL_VSP_AMDP_SERVICE at %d", iTest, iSvc)
	}
}
