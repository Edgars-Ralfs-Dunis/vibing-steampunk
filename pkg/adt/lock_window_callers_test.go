package adt

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

// The lock windows the workflows own are marked, so their mutators skip the
// package lookup (see mutation_gate_marker.go). A window opened by a *caller*
// is not: the MCP tools that take their own lock (#169) and any caller that
// passes a lock_handle from an earlier LockObject both run the mutator's full
// gate after the LOCK. With a package allowlist that gate resolves the package
// over the network, and a stateless lookup there retires the session the handle
// lives in. Found on ZES 050 (NW 7.50) with SAP_ALLOWED_PACKAGES=Z*, where
// DeleteObject answered 423 ExceptionResourceInvalidLockHandle.

const probeProgramURL = "/sap/bc/adt/programs/programs/zdemo_probe"

func lockWindowRoute(w http.ResponseWriter, r *http.Request) {
	switch {
	case strings.Contains(r.URL.Path, "informationsystem/search"):
		_, _ = io.WriteString(w, searchXMLFor(probeProgramURL, "ZDEMO_PROBE", "ZDEMO"))
	case strings.Contains(r.URL.Path, "/checkruns"):
		w.Header().Set("Content-Type", "application/vnd.sap.adt.checkmessages+xml")
		_, _ = io.WriteString(w, testEmptyCheckXML)
	case r.Method == http.MethodPost && r.URL.Query().Get("_action") == "LOCK":
		w.Header().Set("Content-Type", "application/vnd.sap.as+xml")
		_, _ = io.WriteString(w, testLockXML)
	default:
		w.WriteHeader(http.StatusOK)
	}
}

func isObjectDelete(c wireCall) bool {
	return c.method == http.MethodDelete && strings.HasSuffix(c.path, "/zdemo_probe")
}

func TestDeleteObject_CallerLock_NoStatelessRequestBeforeDelete(t *testing.T) {
	rec := &adtRecorder{}
	client := newStubbedClient(t, rec, lockWindowRoute, WithAllowedPackages("Z*"))
	ctx := context.Background()

	lock, err := client.LockObject(ctx, probeProgramURL, "MODIFY")
	if err != nil {
		t.Fatalf("LockObject: %v", err)
	}
	if err := client.DeleteObject(ctx, probeProgramURL, lock.LockHandle, ""); err != nil {
		t.Fatalf("DeleteObject: %v", err)
	}

	calls := rec.snapshot()
	lockAt := indexOfCall(calls, isLock)
	deleteAt := indexOfCall(calls, isObjectDelete)
	if lockAt < 0 || deleteAt < 0 || deleteAt < lockAt {
		t.Fatalf("expected a LOCK followed by a DELETE; trace:\n%v", calls)
	}
	assertWindowStateful(t, calls, lockAt, deleteAt)
}

func TestUpdateSource_CallerLock_NoStatelessRequestBeforePut(t *testing.T) {
	rec := &adtRecorder{}
	client := newStubbedClient(t, rec, lockWindowRoute, WithAllowedPackages("Z*"))
	ctx := context.Background()

	lock, err := client.LockObject(ctx, probeProgramURL, "MODIFY")
	if err != nil {
		t.Fatalf("LockObject: %v", err)
	}
	if err := client.UpdateSource(ctx, probeProgramURL+"/source/main",
		"REPORT zdemo_probe.\n", lock.LockHandle, ""); err != nil {
		t.Fatalf("UpdateSource: %v", err)
	}

	calls := rec.snapshot()
	lockAt := indexOfCall(calls, isLock)
	putAt := indexOfCall(calls, isSourcePut)
	if lockAt < 0 || putAt < 0 || putAt < lockAt {
		t.Fatalf("expected a LOCK followed by a source PUT; trace:\n%v", calls)
	}
	assertWindowStateful(t, calls, lockAt, putAt)
}
