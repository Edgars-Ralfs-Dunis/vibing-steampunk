package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// `--top 0` must mean "all rows" (per the flag's own help text), while
// omitting `--top` entirely must keep the existing 100-row default. Both
// leave the underlying int at its zero value, so the fix depends entirely on
// cmd.Flags().Changed("top") telling the two cases apart. This test pins that
// distinction so a future "simplification" doesn't collapse it back into a
// bare `top == 0` check.
func TestQueryTopFlagChangedDistinguishesExplicitZero(t *testing.T) {
	newQueryFlags := func() *cobra.Command {
		cmd := &cobra.Command{Use: "query"}
		cmd.Flags().Int("top", 0, "Maximum number of rows (0=all)")
		cmd.Flags().Int("skip", 0, "Skip first N rows")
		return cmd
	}

	t.Run("top omitted", func(t *testing.T) {
		cmd := newQueryFlags()
		if err := cmd.ParseFlags([]string{}); err != nil {
			t.Fatal(err)
		}
		if cmd.Flags().Changed("top") {
			t.Fatal("Changed(\"top\") should be false when --top is not passed")
		}
		top, _ := cmd.Flags().GetInt("top")
		if top != 0 {
			t.Fatalf("expected zero value for unset --top, got %d", top)
		}
	})

	t.Run("top explicitly zero", func(t *testing.T) {
		cmd := newQueryFlags()
		if err := cmd.ParseFlags([]string{"--top", "0"}); err != nil {
			t.Fatal(err)
		}
		if !cmd.Flags().Changed("top") {
			t.Fatal("Changed(\"top\") should be true when --top 0 is passed explicitly")
		}
		top, _ := cmd.Flags().GetInt("top")
		if top != 0 {
			t.Fatalf("expected 0, got %d", top)
		}
	})

	t.Run("top explicitly positive", func(t *testing.T) {
		cmd := newQueryFlags()
		if err := cmd.ParseFlags([]string{"--top", "50"}); err != nil {
			t.Fatal(err)
		}
		if !cmd.Flags().Changed("top") {
			t.Fatal("Changed(\"top\") should be true when --top is passed")
		}
		top, _ := cmd.Flags().GetInt("top")
		if top != 50 {
			t.Fatalf("expected 50, got %d", top)
		}
	})
}

// F1-06: a match in a class include is printed at that include; its line
// number counts within it, not within the class's main source.
func TestGrepPrintsTheIncludeAMatchIsIn(t *testing.T) {
	const cls = "/sap/bc/adt/oo/classes/zcl_x"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-CSRF-Token", "t")
		switch r.URL.Path {
		case "/sap/bc/adt/repository/nodestructure":
			w.Write([]byte(`<asx:abap xmlns:asx="http://www.sap.com/abapxml"><asx:values><DATA><TREE_CONTENT><SEU_ADT_REPOSITORY_OBJ_NODE>` +
				`<OBJECT_TYPE>CLAS/OC</OBJECT_TYPE><OBJECT_NAME>ZCL_X</OBJECT_NAME><OBJECT_URI>` + cls + `</OBJECT_URI>` +
				`</SEU_ADT_REPOSITORY_OBJ_NODE></TREE_CONTENT></DATA></asx:values></asx:abap>`))
		case cls + "/source/main":
			w.Write([]byte("class ZCL_X definition.\r\nendclass.\r\n"))
		case cls + "/includes/testclasses":
			w.Write([]byte("CLASS ltc_x DEFINITION FOR TESTING.\r\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	t.Chdir(t.TempDir()) // no .vsp.json: the SAP_* variables below pick the system
	t.Setenv("HOME", t.TempDir())
	t.Setenv("SAP_URL", srv.URL)
	t.Setenv("SAP_USER", "TESTER")
	t.Setenv("SAP_PASSWORD", "secret")
	t.Setenv("SAP_CLIENT_CERT_CN", "")
	t.Setenv("SAP_CLIENT_CERT_ISSUER", "")
	cmd := &cobra.Command{}
	cmd.Flags().String("package", "ZPKG", "")
	cmd.Flags().Bool("ignore-case", false, "")
	cmd.Flags().String("type", "", "")
	cmd.Flags().Int("max", 100, "")

	out := captureStdout(t, func() {
		if err := runGrep(cmd, []string{"ltc_x"}); err != nil {
			t.Error(err)
		}
	})
	if !strings.Contains(out, cls+"/includes/testclasses:1: CLASS ltc_x") {
		t.Fatalf("want the match located in its include, got:\n%s", out)
	}
}
