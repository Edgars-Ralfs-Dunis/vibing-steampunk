package adt

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// grepServer serves sources by path. A path in sources answers 200 with its
// body; a path in status answers that status; anything else is a 404, which is
// what SAP answers for an object that does not exist.
func grepServer(t *testing.T, sources map[string]string, status map[string]int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-CSRF-Token", "test-token")
		if code, ok := status[r.URL.Path]; ok {
			http.Error(w, "status for "+r.URL.Path, code)
			return
		}
		body, ok := sources[r.URL.Path]
		if !ok {
			http.Error(w, "No suitable resource found", http.StatusNotFound)
			return
		}
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

const emptyNodeStructure = `<?xml version="1.0" encoding="utf-8"?>
<asx:abap version="1.0" xmlns:asx="http://www.sap.com/abapxml"><asx:values><DATA><TREE_CONTENT/></DATA></asx:values></asx:abap>`

func nodeStructureWith(objects ...[2]string) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="utf-8"?><asx:abap version="1.0" xmlns:asx="http://www.sap.com/abapxml"><asx:values><DATA><TREE_CONTENT>`)
	for _, o := range objects {
		uri := o[1]
		name := uri[strings.LastIndex(uri, "/")+1:]
		b.WriteString("<SEU_ADT_REPOSITORY_OBJ_NODE><OBJECT_TYPE>" + o[0] + "</OBJECT_TYPE><OBJECT_NAME>" +
			strings.ToUpper(name) + "</OBJECT_NAME><OBJECT_URI>" + uri + "</OBJECT_URI></SEU_ADT_REPOSITORY_OBJ_NODE>")
	}
	b.WriteString(`</TREE_CONTENT></DATA></asx:values></asx:abap>`)
	return b.String()
}

// F1-01: an object that could not be read was counted as searched.
func TestGrepObjectsNamesTheObjectItCouldNotRead(t *testing.T) {
	const ok = "/sap/bc/adt/programs/programs/zok"
	const missing = "/sap/bc/adt/programs/programs/zz_no_such_prog"
	srv := grepServer(t, map[string]string{ok + "/source/main": "REPORT zok.\r\n"}, nil)
	client := NewClient(srv.URL, "user", "pass")

	res, err := client.GrepObjects(context.Background(), []string{ok, missing}, "NEVER_THERE", false, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Unsearched) != 1 || res.Unsearched[0].Object != missing || !strings.Contains(res.Unsearched[0].Reason, "404") {
		t.Fatalf("the unreadable object must be named with its 404, got %+v", res.Unsearched)
	}
	if !res.Partial || !res.Success {
		t.Fatalf("one of two searched: want success and partial, got success=%v partial=%v", res.Success, res.Partial)
	}
	if !strings.Contains(res.Message, "No matches found in 1 object(s)") || !strings.Contains(res.Message, "not a complete answer") {
		t.Fatalf("message must count only what was read and carry the caveat:\n%s", res.Message)
	}

	// Nothing readable at all is not an answer.
	res, err = client.GrepObjects(context.Background(), []string{missing}, ".", false, 0)
	if err != nil {
		t.Fatal(err)
	}
	if res.Success || !res.Partial || len(res.Unsearched) != 1 {
		t.Fatalf("nothing was searched: want success=false partial=true, got %+v", res)
	}
}

// F1-02: a package that does not exist was reported as searched with no matches.
func TestGrepPackageSaysAMissingPackageDoesNotExist(t *testing.T) {
	srv := grepServer(t, map[string]string{"/sap/bc/adt/repository/nodestructure": emptyNodeStructure}, nil)
	client := NewClient(srv.URL, "user", "pass")

	res, err := client.GrepPackage(context.Background(), "ZZZ_NO_SUCH_PKG", ".", false, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if res.Success || !res.Partial || !strings.Contains(res.Message, "does not exist") {
		t.Fatalf("a missing package must fail as missing, got %+v", res)
	}

	all, err := client.GrepPackages(context.Background(), []string{"ZZZ_NO_SUCH_PKG"}, false, ".", false, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if all.Success || !all.Partial || len(all.Unsearched) != 1 || all.Unsearched[0].Object != "ZZZ_NO_SUCH_PKG" {
		t.Fatalf("GrepPackages must not count the missing package as searched, got %+v", all)
	}
}

// An existing package that is empty is a real "no matches", not a failure.
func TestGrepPackageOnAnEmptyExistingPackageIsAnAnswer(t *testing.T) {
	srv := grepServer(t, map[string]string{
		"/sap/bc/adt/repository/nodestructure": emptyNodeStructure,
		"/sap/bc/adt/packages/ZEMPTY":          "<pak:package/>",
	}, nil)
	client := NewClient(srv.URL, "user", "pass")

	res, err := client.GrepPackage(context.Background(), "ZEMPTY", ".", false, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Success || res.Partial {
		t.Fatalf("an empty package was searched completely, got %+v", res)
	}
}

// F1-01 for packages: an object of the package that failed to read is named.
func TestGrepPackageNamesTheObjectItCouldNotRead(t *testing.T) {
	const ok = "/sap/bc/adt/programs/programs/zok"
	const broken = "/sap/bc/adt/programs/programs/zbroken"
	srv := grepServer(t, map[string]string{
		"/sap/bc/adt/repository/nodestructure": nodeStructureWith([2]string{"PROG/P", ok}, [2]string{"PROG/P", broken}),
		ok + "/source/main":                    "REPORT zok.\r\n",
	}, map[string]int{broken + "/source/main": http.StatusInternalServerError})
	client := NewClient(srv.URL, "user", "pass")

	res, err := client.GrepPackage(context.Background(), "ZPKG", "NEVER_THERE", false, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Success || !res.Partial || len(res.Unsearched) != 1 || res.Unsearched[0].Object != broken {
		t.Fatalf("the 500 must land in unsearched, got %+v", res)
	}
	if !strings.Contains(res.Message, "not a complete answer") {
		t.Fatalf("message must carry the caveat:\n%s", res.Message)
	}
}

// F1-07: ADT serves a class include at .../includes/<name>, not under /source/main.
func TestGrepObjectReadsAClassIncludeURL(t *testing.T) {
	const inc = "/sap/bc/adt/oo/classes/zcl_hr_pa9950_fix/includes/testclasses"
	srv := grepServer(t, map[string]string{inc: `*"* use this source file for your ABAP unit test classes`}, nil)
	client := NewClient(srv.URL, "user", "pass")

	res, err := client.GrepObject(context.Background(), inc, ".", false, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Success || res.MatchCount != 1 {
		t.Fatalf("want 1 match from the include itself, got %+v", res)
	}
}

// F1-06: a class grep also reads the local definitions, implementations,
// macros and test classes, and says which include each match came from.
func TestGrepObjectReadsTheLocalIncludesOfAClass(t *testing.T) {
	const cls = "/sap/bc/adt/oo/classes/zcl_ita2_classifier"
	srv := grepServer(t, map[string]string{
		cls + "/source/main":              "class ZCL_ITA2_CLASSIFIER definition\r\n  public.\r\nendclass.\r\n",
		cls + "/includes/implementations": "* local helpers\r\n",
		cls + "/includes/testclasses": "CLASS ltd_classifier DEFINITION FOR TESTING.\r\nENDCLASS.\r\n" +
			"CLASS ltc_classifier DEFINITION FINAL FOR TESTING.\r\nENDCLASS.\r\n" +
			"CLASS ltc_damaged_deletion DEFINITION FINAL FOR TESTING.\r\nENDCLASS.\r\n",
		// definitions: 404, a class without that include
	}, map[string]int{cls + "/includes/macros": http.StatusInternalServerError})
	client := NewClient(srv.URL, "user", "pass")

	res, err := client.GrepObject(context.Background(), cls, `(?i)^CLASS\s+lt\w+\s+DEFINITION`, false, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Success || res.MatchCount != 3 {
		t.Fatalf("want the 3 local test classes, got %+v", res)
	}
	if res.Matches[0].Source != cls+"/includes/testclasses" || res.Matches[0].LineNumber != 1 {
		t.Fatalf("a match must name its include and keep the include's line number, got %+v", res.Matches[0])
	}
	if len(res.Unsearched) != 1 || res.Unsearched[0].Object != cls+"/includes/macros" {
		t.Fatalf("the include that failed with 500 must be named; a 404 is just an absent include. got %+v", res.Unsearched)
	}
}

// F1-04, F1-05: a function group's /source/main is only the SAPL frame; the
// grep must reach its includes and function modules, also from a package.
func TestGrepReachesTheFunctionModulesOfAGroup(t *testing.T) {
	const fg = "/sap/bc/adt/functions/groups/zvsp_demo"
	srv := grepServer(t, map[string]string{
		fg + "/objectstructure":                      fugrStructure,
		fg + "/source/main":                          "FUNCTION-POOL zvsp_demo.\r\nINCLUDE lzvsp_demotop.\r\n",
		fg + "/includes/lzvsp_demotop/source/main":   "FUNCTION-POOL zvsp_demo.\r\n",
		fg + "/fmodules/z_vsp_demo_call/source/main": "FUNCTION z_vsp_demo_call.\r\nENDFUNCTION.\r\n",
		"/sap/bc/adt/repository/nodestructure":       nodeStructureWith([2]string{"FUGR/F", fg}),
	}, nil)
	client := NewClient(srv.URL, "user", "pass")

	objs, err := client.GrepObjects(context.Background(), []string{fg}, `(?i)^\s*FUNCTION\s`, false, 0)
	if err != nil {
		t.Fatal(err)
	}
	if objs.TotalMatches != 1 || objs.Objects[0].Matches[0].Source != fg+"/fmodules/z_vsp_demo_call/source/main" {
		t.Fatalf("want the FUNCTION line of the module, got %+v", objs)
	}

	pkg, err := client.GrepPackage(context.Background(), "ZPKG", `(?i)^\s*FUNCTION\s`, false, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if pkg.TotalMatches != 1 || pkg.Searched["FUGR/F"] != 1 {
		t.Fatalf("want 1 match and the group counted under FUGR/F, got %+v", pkg)
	}
}

// F1-08: ADT sends CRLF; a $-anchored pattern must still match, and the
// matched line must not carry the CR.
func TestGrepObjectAnchorsAtEndOfLineInCRLFSource(t *testing.T) {
	const fm = "/sap/bc/adt/functions/groups/btch/fmodules/job_submit"
	srv := grepServer(t, map[string]string{fm + "/source/main": "FUNCTION JOB_SUBMIT\r\n  WRITE 'x'.\r\nENDFUNCTION.\r\n"}, nil)
	client := NewClient(srv.URL, "user", "pass")

	res, err := client.GrepObject(context.Background(), fm, `^ENDFUNCTION\.$`, false, 1)
	if err != nil {
		t.Fatal(err)
	}
	if res.MatchCount != 1 || res.Matches[0].MatchedLine != "ENDFUNCTION." || res.Matches[0].ContextBefore[0] != "  WRITE 'x'." {
		t.Fatalf("want 1 CR-free match, got %+v", res)
	}
}

// F1-11: a walk that max_results stopped must say so.
func TestGrepPackageSaysWhenMaxResultsStoppedIt(t *testing.T) {
	const a, b, c = "/sap/bc/adt/programs/programs/za", "/sap/bc/adt/programs/programs/zb", "/sap/bc/adt/programs/programs/zc"
	srv := grepServer(t, map[string]string{
		"/sap/bc/adt/repository/nodestructure": nodeStructureWith([2]string{"PROG/P", a}, [2]string{"PROG/P", b}, [2]string{"PROG/P", c}),
		a + "/source/main":                     "REPORT za.\r\n",
		b + "/source/main":                     "REPORT zb.\r\n",
		c + "/source/main":                     "REPORT zc.\r\n",
	}, nil)
	client := NewClient(srv.URL, "user", "pass")

	res, err := client.GrepPackage(context.Background(), "ZPKG", "REPORT", false, nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Objects) != 1 || !res.Partial || !strings.Contains(res.Message, "max_results=1") {
		t.Fatalf("want 1 object and a max_results note, got %+v", res)
	}

	all, err := client.GrepPackages(context.Background(), []string{"ZPKG"}, false, "REPORT", false, nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(all.Objects) != 1 || !all.Partial || !strings.Contains(all.Message, "max_results=1") {
		t.Fatalf("want 1 object and a max_results note, got %+v", all)
	}

	// A walk that finished under the cap is complete and says nothing.
	res, err = client.GrepPackage(context.Background(), "ZPKG", "REPORT", false, nil, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Objects) != 3 || res.Partial || strings.Contains(res.Message, "max_results") {
		t.Fatalf("want 3 objects and no note, got %+v", res)
	}
}

// F1-10: a package grep reads objects in parallel and, at its deadline,
// returns what it found plus what it did not reach instead of running on.
func TestGrepPackageIsParallelAndStopsAtItsDeadline(t *testing.T) {
	var objects [][2]string
	for _, n := range []string{"za", "zb", "zc", "zd", "ze", "zf", "zg", "zslow"} {
		objects = append(objects, [2]string{"PROG/P", "/sap/bc/adt/programs/programs/" + n})
	}
	var inFlight, peak atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-CSRF-Token", "test-token")
		if r.URL.Path == "/sap/bc/adt/repository/nodestructure" {
			w.Write([]byte(nodeStructureWith(objects...)))
			return
		}
		n := inFlight.Add(1)
		defer inFlight.Add(-1)
		for p := peak.Load(); n > p && !peak.CompareAndSwap(p, n); p = peak.Load() {
		}
		delay := 50 * time.Millisecond
		if strings.Contains(r.URL.Path, "zslow") {
			delay = 2 * time.Second
		}
		select {
		case <-time.After(delay):
		case <-r.Context().Done():
			return
		}
		w.Write([]byte("REPORT x.\r\n"))
	}))
	defer srv.Close()
	client := NewClient(srv.URL, "user", "pass")

	old := grepPackageDeadline
	grepPackageDeadline = 500 * time.Millisecond
	defer func() { grepPackageDeadline = old }()

	start := time.Now()
	res, err := client.GrepPackage(context.Background(), "ZPKG", "REPORT", false, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if took := time.Since(start); took > 1500*time.Millisecond {
		t.Fatalf("the deadline did not stop the walk: took %v", took)
	}
	if peak.Load() < 2 {
		t.Fatalf("objects were read one at a time")
	}
	if len(res.Objects) != 7 || !res.Partial || len(res.Unsearched) != 1 || !strings.Contains(res.Unsearched[0].Object, "zslow") {
		t.Fatalf("want 7 matches and zslow unsearched, got %+v", res)
	}
}

// F1-09: like grep -C, overlapping context windows show each line once.
func TestGrepObjectContextWindowsDoNotRepeatLines(t *testing.T) {
	const inc = "/sap/bc/adt/programs/includes/mpe00f00"
	srv := grepServer(t, map[string]string{inc + "/source/main": "l1\nl2\nFORM a.\nl4\nFORM b.\nl6\nl7\nl8\nl9\nl10"}, nil)
	client := NewClient(srv.URL, "user", "pass")

	res, err := client.GrepObject(context.Background(), inc, "^FORM", false, 2)
	if err != nil {
		t.Fatal(err)
	}
	var shown []string
	for _, m := range res.Matches {
		shown = append(append(append(shown, m.ContextBefore...), m.MatchedLine), m.ContextAfter...)
	}
	if got := strings.Join(shown, ","); got != "l1,l2,FORM a.,l4,FORM b.,l6,l7" {
		t.Fatalf("want each line once, in order, got %s", got)
	}
}
