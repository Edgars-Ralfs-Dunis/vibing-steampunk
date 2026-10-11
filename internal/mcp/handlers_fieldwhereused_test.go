package mcp

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func classInclude(class, part string) string {
	return class + strings.Repeat("=", 30-len(class)) + part
}

// fieldUsageServer answers the DDIC data preview like SAP: the rows of the
// table named in ddicEntityName (the first row is the header), cut at
// rowNumber. It records every request as "TABLE rowNumber: statement".
func fieldUsageServer(t *testing.T, tables map[string][][]string) (*httptest.Server, *[]string) {
	t.Helper()
	var sent []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-csrf-token", "test-token")
		if r.Method != http.MethodPost {
			return
		}
		q := r.URL.Query()
		body, _ := io.ReadAll(r.Body)
		sent = append(sent, q.Get("ddicEntityName")+" "+q.Get("rowNumber")+": "+string(body))
		table := tables[q.Get("ddicEntityName")]
		rows := table[1:]
		if n, _ := strconv.Atoi(q.Get("rowNumber")); len(rows) > n {
			rows = rows[:n]
		}
		var b strings.Builder
		b.WriteString(`<dataPreview:tableData xmlns:dataPreview="http://www.sap.com/adt/dataPreview">`)
		for i, name := range table[0] {
			b.WriteString(`<dataPreview:columns><dataPreview:metadata dataPreview:name="` + name + `"/><dataPreview:dataSet>`)
			for _, row := range rows {
				b.WriteString(`<dataPreview:data>` + row[i] + `</dataPreview:data>`)
			}
			b.WriteString(`</dataPreview:dataSet></dataPreview:columns>`)
		}
		b.WriteString(`</dataPreview:tableData>`)
		w.Write([]byte(b.String()))
	}))
	return srv, &sent
}

func fieldUsageCall(t *testing.T, srv *httptest.Server, args map[string]any) (string, bool) {
	t.Helper()
	// Read-only, as the night server runs: the tool must be a read.
	s := NewServer(&Config{BaseURL: srv.URL, Username: "TESTUSER", Client: "001", Mode: "expert", ReadOnly: true})
	result, err := s.handleFieldWhereUsed(t.Context(), newRequest(args))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return resultText(result), result.IsError
}

func TestFieldWhereUsed(t *testing.T) {
	srv, sent := fieldUsageServer(t, map[string][][]string{
		"WBCROSSGT": {{"INCLUDE"},
			{classInclude("ZCL_DEMO", "CM00A")},
			{classInclude("ZCL_DEMO", "CM010")},
			{classInclude("ZCL_DEMO", "CU")},
			{classInclude("ZCL_DEMO", "CCAU")},
			{classInclude("ZCL_ONLY_TESTS", "CCAU")},
			{classInclude("ZCL_DEMO", "CM00B")}, // TMDIR does not know it
			{"ZREPORT"}, {"ZREPORT_TOP"}, {"SAPLZFG"}, {"ZGONE"},
		},
		"TMDIR": {{"CLASSNAME", "METHODINDX", "METHODNAME"},
			// zero-padded like SAP, plus a cross-product row nobody asked for
			{"ZCL_DEMO", "00010", "READ_CALCR"}, {"ZCL_DEMO", "00036", "CHECK_CALCR"}, {"ZCL_DEMO", "00002", "NOISE"}},
		"TRDIR": {{"NAME", "SUBC"}, {"ZREPORT", "1"}, {"ZREPORT_TOP", "I"}, {"SAPLZFG", "F"}},
	})
	defer srv.Close()

	text, isErr := fieldUsageCall(t, srv, map[string]any{"table": "t549a", "field": "calcr"})
	if isErr {
		t.Fatalf("refused: %s", text)
	}
	want := []string{
		`WBCROSSGT 1001: SELECT INCLUDE FROM WBCROSSGT WHERE OTYPE = 'TY' AND NAME = 'T549A\TY:CALCR'`,
		`TMDIR 3: SELECT CLASSNAME, METHODINDX, METHODNAME FROM TMDIR WHERE CLASSNAME IN ( 'ZCL_DEMO' ) AND METHODINDX IN ( 10, 11, 36 )`,
		`TRDIR 4: SELECT NAME, SUBC FROM TRDIR WHERE NAME IN ( 'SAPLZFG', 'ZGONE', 'ZREPORT', 'ZREPORT_TOP' )`,
	}
	if strings.Join(*sent, "\n") != strings.Join(want, "\n") {
		t.Errorf("statements sent:\n%s\nwant:\n%s", strings.Join(*sent, "\n"), strings.Join(want, "\n"))
	}

	// Compact JSON, in exactly this shape.
	wantJSON := `{"table":"T549A","field":"CALCR","index_rows":10,"truncated":false,` +
		`"classes":[{"class":"ZCL_DEMO","methods":["CHECK_CALCR","CM00B","READ_CALCR"],"parts":["CU"]}],` +
		`"programs":["ZREPORT"],"includes":["ZREPORT_TOP"],` +
		`"other":[{"name":"SAPLZFG","subc":"F"},{"name":"ZGONE","subc":""}],` +
		`"counts":{"classes":1,"methods":3,"programs":1,"includes":1,"other":2,"unit_test_includes_left_out":2}}`
	if text != wantJSON {
		t.Errorf("answer:\n%s\nwant:\n%s", text, wantJSON)
	}
}

func TestFieldWhereUsedSaysWhenItCutTheIndex(t *testing.T) {
	srv, sent := fieldUsageServer(t, map[string][][]string{
		"WBCROSSGT": {{"INCLUDE"}, {"ZA"}, {"ZB"}, {"ZC"}},
		"TRDIR":     {{"NAME", "SUBC"}, {"ZA", "1"}, {"ZB", "1"}},
	})
	defer srv.Close()

	text, _ := fieldUsageCall(t, srv, map[string]any{"table": "T549A", "field": "CALCR", "max_results": float64(2)})
	var got map[string]any
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatalf("not JSON: %s", text)
	}
	if !strings.HasPrefix((*sent)[0], "WBCROSSGT 3: ") {
		t.Errorf("must ask for one row more than it keeps, sent %q", (*sent)[0])
	}
	if got["truncated"] != true || got["index_rows"] != float64(2) || !strings.Contains(got["note"].(string), "more than 2") {
		t.Errorf("a cut list must say so: %s", text)
	}
}

func TestFieldWhereUsedCapsMaxResultsBelowTheDataPreviewCutoff(t *testing.T) {
	srv, sent := fieldUsageServer(t, map[string][][]string{"WBCROSSGT": {{"INCLUDE"}}})
	defer srv.Close()

	fieldUsageCall(t, srv, map[string]any{"table": "T549A", "field": "CALCR", "max_results": float64(1000000)})
	if !strings.HasPrefix((*sent)[0], "WBCROSSGT 90000: ") {
		t.Errorf("rowNumber above 90000 comes back as ~100 rows that look complete, sent %q", (*sent)[0])
	}
}

func TestFieldWhereUsedEmptyIndexPointsToGrep(t *testing.T) {
	srv, _ := fieldUsageServer(t, map[string][][]string{"WBCROSSGT": {{"INCLUDE"}}})
	defer srv.Close()

	text, _ := fieldUsageCall(t, srv, map[string]any{"table": "T549A", "field": "CALCR"})
	if !strings.Contains(text, `"truncated":false`) || !strings.Contains(text, "incomplete") || !strings.Contains(text, "grep") {
		t.Errorf("an empty index is not proof of no use: %s", text)
	}
}

func TestFieldWhereUsedRefusesWhatIsNotAName(t *testing.T) {
	srv, sent := fieldUsageServer(t, nil)
	defer srv.Close()

	for _, args := range []map[string]any{
		{"table": "T549A' OR '1'='1", "field": "CALCR"},
		{"table": "T549A", "field": "CALCR' OR '1'='1"},
		{"table": "T549A", "field": "/X/F"},
		{"table": "", "field": "CALCR"},
	} {
		if text, isErr := fieldUsageCall(t, srv, args); !isErr {
			t.Errorf("%v was accepted: %s", args, text)
		}
	}
	if len(*sent) > 0 {
		t.Errorf("a refused name must send nothing, sent %q", *sent)
	}
}
