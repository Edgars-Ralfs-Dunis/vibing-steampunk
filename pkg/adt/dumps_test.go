package adt

import (
	"strings"
	"testing"
	"time"
)

const dumpFeedSample = `<?xml version="1.0" encoding="utf-8"?>
<atom:feed xmlns:atom="http://www.w3.org/2005/Atom">
 <atom:entry xml:lang="EN">
  <atom:author FullName="Test User"><atom:name>TESTUSER</atom:name></atom:author>
  <atom:category term="CALL_FUNCTION_NOT_REMOTE" label="ABAP runtime error"/>
  <atom:category term="SAPLSBAL_DB" label="Terminated ABAP program"/>
  <atom:id>/sap/bc/adt/vit/runtime/dumps/20260822230807</atom:id>
  <atom:title>Function module "BAL_DB_SEARCH" cannot be called 'remotely'.</atom:title>
  <atom:published>2026-08-22T23:08:07Z</atom:published>
 </atom:entry>
 <atom:entry xml:lang="EN">
  <atom:author><atom:name>TESTUSER</atom:name></atom:author>
  <atom:category term="SYNTAX_ERROR" label="ABAP runtime error"/>
  <atom:category term="ZCL_DEMO_THING===============CP" label="Terminated ABAP program"/>
  <atom:id>/sap/bc/adt/vit/runtime/dumps/20260822220000</atom:id>
  <atom:title>Syntax error in program</atom:title>
  <atom:published>2026-08-22T22:00:00Z</atom:published>
 </atom:entry>
</atom:feed>`

// The categories are the structured part, and they are read by label rather
// than by position: a release that adds a third category must not shift the
// meaning of the first two.
func TestDumpFeedReadsCategoriesByLabel(t *testing.T) {
	dumps, err := parseDumpFeed([]byte(dumpFeedSample))
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}
	if len(dumps) != 2 {
		t.Fatalf("expected 2 dumps, got %d", len(dumps))
	}
	first := dumps[0]
	if first.ErrorType != "CALL_FUNCTION_NOT_REMOTE" {
		t.Fatalf("error type is %q", first.ErrorType)
	}
	if first.Program != "SAPLSBAL_DB" {
		t.Fatalf("terminated program is %q", first.Program)
	}
	if first.User != "TESTUSER" {
		t.Fatalf("user is %q", first.User)
	}
	if first.At.Format(time.RFC3339) != "2026-08-22T23:08:07Z" {
		t.Fatalf("timestamp is %s", first.At)
	}
	if first.Message == "" {
		t.Fatal("the message text should survive")
	}
}

func TestDumpFeedSurvivesAnEmptyFeed(t *testing.T) {
	dumps, err := parseDumpFeed([]byte(`<atom:feed xmlns:atom="http://www.w3.org/2005/Atom"/>`))
	if err != nil {
		t.Fatalf("an empty feed is not an error: %v", err)
	}
	if len(dumps) != 0 {
		t.Fatalf("expected nothing, got %d", len(dumps))
	}
}

// Grouping answers "is this new and how often" — which needs the same failure
// collapsed, not the same afternoon. A busy hour would otherwise make unrelated
// failures look like one incident.
func TestGroupingCollapsesTheSameFailure(t *testing.T) {
	at := func(min int) time.Time {
		return time.Date(2026, 8, 22, 10, min, 0, 0, time.UTC)
	}
	groups := GroupDumps([]Dump{
		{ErrorType: "SYNTAX_ERROR", Program: "ZCL_A", User: "ONE", At: at(1)},
		{ErrorType: "SYNTAX_ERROR", Program: "ZCL_A", User: "TWO", At: at(9)},
		{ErrorType: "SYNTAX_ERROR", Program: "ZCL_A", User: "ONE", At: at(5)},
		{ErrorType: "SYNTAX_ERROR", Program: "ZCL_B", User: "ONE", At: at(3)},
	})
	if len(groups) != 2 {
		t.Fatalf("two distinct failures, got %d", len(groups))
	}
	top := groups[0]
	if top.Program != "ZCL_A" || top.Count != 3 {
		t.Fatalf("the most frequent should lead: %+v", top)
	}
	if !top.First.Equal(at(1)) || !top.Last.Equal(at(9)) {
		t.Fatalf("the window should span the group: %s..%s", top.First, top.Last)
	}
	if len(top.Users) != 2 {
		t.Fatalf("both users should be listed, got %v", top.Users)
	}
}

// The whole argument of the design: a log written by the program that dumped
// outranks one that merely happened nearby, however near.
func TestProgramIdentityOutranksProximity(t *testing.T) {
	dumpAt := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	dump := Dump{Program: "ZCL_DEMO_ORDER", User: "TESTUSER", At: dumpAt}

	sameProgramFarAway := AppLogEntry{Program: "ZCL_DEMO_ORDER", User: "OTHER", At: dumpAt.Add(-4 * time.Minute)}
	sameUserRightBefore := AppLogEntry{Program: "SAPMHTTP", User: "TESTUSER", At: dumpAt.Add(-1 * time.Second)}

	farScore, farWhy := rankLogAgainstDump(sameProgramFarAway, dump, nil, dumpAt.Sub(sameProgramFarAway.At))
	nearScore, _ := rankLogAgainstDump(sameUserRightBefore, dump, nil, dumpAt.Sub(sameUserRightBefore.At))

	if farScore <= nearScore {
		t.Fatalf("the program that dumped should outrank a coincidence one second away: %d vs %d", farScore, nearScore)
	}
	if farWhy == "" {
		t.Fatal("a ranking without a stated reason cannot be overruled by a person")
	}
}

// Direction, not just distance. A log written after the failure is error
// handling; calling it a cause would be backwards.
func TestALogAfterTheDumpRanksBelowOneBefore(t *testing.T) {
	dumpAt := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	dump := Dump{Program: "ZCL_DEMO_ORDER", User: "TESTUSER", At: dumpAt}

	before := AppLogEntry{Program: "OTHER", User: "TESTUSER", At: dumpAt.Add(-2 * time.Second)}
	after := AppLogEntry{Program: "OTHER", User: "TESTUSER", At: dumpAt.Add(2 * time.Second)}

	beforeScore, _ := rankLogAgainstDump(before, dump, nil, dumpAt.Sub(before.At))
	afterScore, afterWhy := rankLogAgainstDump(after, dump, nil, dumpAt.Sub(after.At))

	if beforeScore <= afterScore {
		t.Fatalf("before should outrank after: %d vs %d", beforeScore, afterScore)
	}
	if afterWhy == "" || !strings.Contains(afterWhy, "not cause") {
		t.Fatalf("the reason should say why it is weaker, got %q", afterWhy)
	}
}

// A class dies as its pool, ZCL_X=====CP, and the caller has the class name.
// Filtering on it returned no dumps at all, which reads as "no dumps".
func TestDumpProgramFilterMatchesAClassByItsPool(t *testing.T) {
	d := Dump{Program: "ZCL_DEMO_THING===============CP"}
	for _, want := range []string{"zcl_demo_thing", "ZCL_DEMO_THING===============CP"} {
		if !(DumpFilter{Program: want}).matches(d) {
			t.Errorf("program %q did not match %s", want, d.Program)
		}
	}
	if (DumpFilter{Program: "ZCL_DEMO"}).matches(d) {
		t.Error("a prefix of the class name matched another class's pool")
	}
}
