package adt

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
