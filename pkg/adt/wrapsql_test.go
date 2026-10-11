package adt

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestWrapSQL(t *testing.T) {
	// A long IN list with a literal that must not be split, and a long
	// ORDER BY that the data preview once cut into "sdlt" and "ime".
	var lits []string
	for i := 0; i < 40; i++ {
		lits = append(lits, "'0aAi4G0H7{2vu4jDCI}OeW'")
	}
	q := "SELECT a, b FROM t WHERE x IN ( " + strings.Join(lits, ", ") + " ) AND y = 'a value with spaces inside' ORDER BY sdldate DESCENDING, sdltime DESCENDING"
	w := wrapSQL(q)
	for n, line := range strings.Split(w, "\n") {
		if len(line) > 200 {
			t.Errorf("line %d is %d characters", n+1, len(line))
		}
	}
	if strings.ReplaceAll(w, "\n", " ") != q {
		t.Error("wrapping changed the statement's words")
	}
	if !strings.Contains(w, "'a value with spaces inside'") {
		t.Error("a literal was broken")
	}
	if strings.Count(w, "\n") < 4 {
		t.Errorf("expected several lines, got %d", strings.Count(w, "\n")+1)
	}
	if wrapSQL("SELECT 1 FROM t") != "SELECT 1 FROM t" {
		t.Error("a short statement changed")
	}
}

// On ZED, 2026-10-11, RunQuery with a condition starting after character 255
// of one line counted 111 instead of 1: SAP cut the line and the condition
// was lost.
func TestWrapSQLConditionAt256(t *testing.T) {
	head := "SELECT COUNT( * ) AS n FROM t000 WHERE mtext <> '"
	head += strings.Repeat("x", 255-len(head)-2) + "' "
	q := head + "AND mandt = '050'"
	if strings.Index(q, "AND mandt") != 255 {
		t.Fatalf("the condition starts at character %d, want 256", strings.Index(q, "AND mandt")+1)
	}
	w := wrapSQL(q)
	for n, line := range strings.Split(w, "\n") {
		if len(line) > 255 {
			t.Errorf("line %d is %d characters", n+1, len(line))
		}
	}
	if strings.ReplaceAll(w, "\n", " ") != q {
		t.Errorf("wrapping changed the statement:\n%s", w)
	}
}

// inList is night 2026-10-11 F2-01's statement: 25 real requests, no blanks,
// so the IN list is one 326-character word. On ZED the ddic preview cut it at
// 255, split 'DVDK9ADQQT' in two, and counted 24 instead of 25.
const inList = "SELECT COUNT( * ) AS n FROM e070 WHERE trkorr IN ('DVQK900593','DVPK900392','DVKK900275','DVEK900054','DVEK900014','DVEK900013','DVD_P00220','DVD_P00208','DVDK9ADSPV','DVDK9ADSPU','DVDK9ADSPS','DVDK9ADSPP','DVDK9ADSGR','DVDK9ADSBC','DVDK9ADS8H','DVDK9ADS8B','DVDK9ADQUB','DVDK9ADQU8','DVDK9ADQTY','DVDK9ADQQT','DVDK9ADQO7','DVDK9ADQNH','DVDK9ADQL0','DVDK9ADQKY','DVDK9ADQKX')"

// sameStatement says a and b differ only in blanks and line breaks outside
// the literals, and every literal of b is whole in a.
func sameStatement(t *testing.T, a, b string) {
	t.Helper()
	if strings.Join(strings.Fields(a), "") != strings.Join(strings.Fields(b), "") {
		t.Errorf("the statement changed:\n%s", a)
	}
	for _, lit := range strings.Split(b, "'")[1:] {
		if len(lit) == 10 && !strings.Contains(a, "'"+lit+"'") {
			t.Errorf("literal %q was split:\n%s", lit, a)
		}
	}
}

func TestWrapSQLBreaksBlankFreeINList(t *testing.T) {
	w := wrapSQL(inList)
	for n, line := range strings.Split(w, "\n") {
		if len(line) > 200 {
			t.Errorf("line %d is %d characters", n+1, len(line))
		}
	}
	sameStatement(t, w, inList)
}

// Both data preview paths send the statement wrapped, and refuse locally one
// that still has a line SAP would cut (night 2026-10-11 F2-01, F2-05).
func TestDataPreviewSQLIsWrappedOrRefused(t *testing.T) {
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-csrf-token", "AbCdEfGhIjKlMnOpQrStUv==")
		if strings.Contains(r.URL.Path, "/datapreview/") {
			b, _ := io.ReadAll(r.Body)
			bodies = append(bodies, string(b))
		}
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<tableData/>`))
	}))
	defer srv.Close()
	cfg := NewConfig(srv.URL, "TESTUSER", "secret")
	c := NewClientWithTransport(cfg, NewTransport(cfg))
	ctx := context.Background()

	if _, err := c.GetTableContents(ctx, "E070", 10, inList); err != nil {
		t.Fatal(err)
	}
	if _, err := c.RunQuery(ctx, inList, 10); err != nil {
		t.Fatal(err)
	}
	if len(bodies) != 2 {
		t.Fatalf("sent %d statements, want 2", len(bodies))
	}
	// ddic wraps at blanks itself, so it gets blanks and no word SAP would
	// cut; freestyle cuts each line, so it gets no line SAP would cut.
	ddic, freestyle := bodies[0], bodies[1]
	for _, word := range strings.Fields(ddic) {
		if utf8.RuneCountInString(word) > 255 {
			t.Errorf("ddic: a word of %d characters was sent", len(word))
		}
	}
	if strings.Contains(ddic, "\n") {
		t.Error("ddic: a line break was sent")
	}
	for n, line := range strings.Split(freestyle, "\n") {
		if utf8.RuneCountInString(line) > 255 {
			t.Errorf("freestyle: line %d of %d characters was sent", n+1, len(line))
		}
	}
	sameStatement(t, ddic, inList)
	sameStatement(t, freestyle, inList)

	long := "SELECT * FROM t000 WHERE mtext = '" + strings.Repeat("x", 300) + "'"
	bodies = nil
	if _, err := c.GetTableContents(ctx, "T000", 10, long); err == nil || !strings.Contains(err.Error(), "255") {
		t.Errorf("GetTableContents error = %v, want a refusal naming 255", err)
	}
	if _, err := c.RunQuery(ctx, long, 10); err == nil || !strings.Contains(err.Error(), "255") {
		t.Errorf("RunQuery error = %v, want a refusal naming 255", err)
	}
	if len(bodies) != 0 {
		t.Errorf("a statement SAP would cut was sent: %q", bodies)
	}
}

// A blank-free word is broken after its commas only outside quotes: a literal
// full of commas must reach SAP whole on both data preview paths. askzes
// (ZES) and the night run (ZED, 2026-10-11) both found that wrapping at blanks
// alone fails on a blank-free IN list; breaking after its commas fixes it.
func TestWrapSQLNeverBreaksInsideALiteral(t *testing.T) {
	lit := "'" + strings.Repeat("A,", 60) + "'"
	q := "SELECT COUNT( * ) FROM t000 WHERE mtext IN (" + lit + "," + lit + "," + lit + ")"
	w := wrapSQL(q)
	for n, line := range strings.Split(w, "\n") {
		if len(line) > 255 {
			t.Errorf("line %d is %d characters", n+1, len(line))
		}
	}
	if got := strings.Count(w, lit); got != 3 {
		t.Errorf("%d of 3 comma-filled literals arrived whole:\n%s", got, w)
	}
}
