package mcp

import (
	"strings"
	"testing"

	"github.com/oisee/vibing-steampunk/pkg/adt"
)

// objectstructure types members CLAS/OM, CLAS/OA and so on; grouping on
// "METH" or "ATTR" put every member of ZCL_ADT_HTTP_GATE under Other Components.
func TestClassComponentsGroupByADTTypeCode(t *testing.T) {
	out := formatClassComponents(&adt.ClassComponent{Name: "ZCL_ADT_HTTP_GATE", Type: "CLAS/OC",
		Components: []adt.ClassComponent{
			{Name: "IS_AUTHORIZED", Type: "CLAS/OM", Visibility: "public", IsStatic: true},
			{Name: "C_AUTH_OBJECT", Type: "CLAS/OA", Visibility: "public", Constant: true},
			{Name: "ON_DENIED", Type: "CLAS/OO", Visibility: "public"},
			{Name: "TY_RULE", Type: "CLAS/OT", Visibility: "private"},
			{Name: "IF_HTTP_EXTENSION", Type: "CLAS/OR", Visibility: "public"},
		}})
	for _, want := range []string{"## Methods (1)", "## Attributes (1)", "## Events (1)", "## Types (1)", "## Other Components (1)"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}
