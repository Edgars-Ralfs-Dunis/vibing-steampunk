package adt

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Correlating a dump with the application log is a time join, and a time join
// is where a tool starts lying. Two things happening in the same second is not
// causation, and "the cause was X" printed from proximity will be confidently
// wrong on the first hard case — which is exactly when someone will believe it.
//
// What rescues it is that a log entry records the program that wrote it. If
// that program is the one that died, the connection is structural, not
// coincidental. So time is the filter and the program is the reason, rather
// than the other way round.

// LogMatch is one application log entry offered as related to a dump, with the
// argument for why.
type LogMatch struct {
	Entry AppLogEntry `json:"entry"`
	// Score ranks the match. Higher is stronger; see the constants below.
	Score int `json:"score"`
	// Why is the argument, in words, so a person can overrule the ranking.
	Why string `json:"why"`
	// Offset is how long before the dump the entry was written. Negative means
	// after it.
	Offset time.Duration `json:"-"`
}

// The ladder. The top two rungs are structural: they say the writing program
// was on the path, not merely nearby. The lower ones are the clock, and are
// scored low on purpose.
const (
	// scoreSameProgram: the log was written by the program that dumped.
	scoreSameProgram = 100
	// scoreOnStack: written by a program on the dump's call stack. On the
	// causal path by construction — the stack is what was running.
	scoreOnStack = 80
	// scoreSameUserBefore: same user, shortly before the dump. Weak, and
	// honestly weak: one person doing one thing produces many such rows.
	scoreSameUserBefore = 40
	// scoreSameUserAfter: same user, after the dump. Weaker still, and kept
	// apart deliberately — a log written after the failure is cleanup or error
	// handling, not cause. Sorting purely by distance in seconds loses that.
	scoreSameUserAfter = 20
	// scoreInWindow: nothing but the clock.
	scoreInWindow = 10
)

// CorrelateDump finds application log entries around a dump and ranks them.
//
// The tolerance is a window on both sides: before, because that is where a
// cause would be, and after, because error handling writes there and is worth
// seeing even though it explains nothing.
func (c *Client) CorrelateDump(ctx context.Context, dump Dump, tolerance time.Duration, limit int) ([]LogMatch, error) {
	// The stack is the strongest evidence available without a call graph, and
	// it costs one request. A dump that will not give up its stack is not a
	// reason to refuse the correlation — it only means the top rung is unused.
	var stack []DumpFrame
	if dump.ID != "" {
		if frames, err := c.DumpStack(ctx, dump.ID); err == nil {
			stack = frames
		}
	}
	// One level down from each frame. This is the rung below the stack: what
	// those frames called has returned already, so it is not on the path at the
	// moment of failure, but it is where a bad value is usually prepared.
	//
	// This rung was dead for as long as it existed, and it is worth recording
	// why rather than deleting the history: it asked GetCalleesOf, which asked
	// /sap/bc/adt/cai/callgraph, a resource advertised on none of 7.50, 7.57 or
	// 7.58 that answers 404. Every frame "contributed nothing" and the rung was
	// silently always empty. That silence was mine — I wrote the swallow as
	// graceful degradation for a resource that turns out never to be there.
	//
	// It fires now. calleesOfStack reads the CROSS and WBCROSSGT
	// cross-reference tables, which is what the note here said would revive it,
	// and CalleesUnavailable now probes those tables rather than the resource
	// that never existed — so a system where free SQL is blocked still says so
	// instead of pretending.
	callees := c.calleesOfStack(ctx, stack)
	// BALHDR's ALDATE/ALTIME are the server's clock and dump.At is UTC. The
	// id's stamp is that same server clock, exact for this dump's own date.
	if local := DumpTimeFromID(dump.ID); !local.IsZero() {
		dump.At = local
	}
	return c.correlateWith(ctx, dump, stack, callees, tolerance, limit)
}

// correlateWithStack is the body, taking the stack as an argument so it can be
// tested without a system.
func (c *Client) correlateWithStack(ctx context.Context, dump Dump, stack []DumpFrame, tolerance time.Duration, limit int) ([]LogMatch, error) {
	return c.correlateWith(ctx, dump, stack, nil, tolerance, limit)
}

func (c *Client) correlateWith(ctx context.Context, dump Dump, stack []DumpFrame, callees map[string]string, tolerance time.Duration, limit int) ([]LogMatch, error) {
	if dump.At.IsZero() {
		return nil, fmt.Errorf("this dump carries no timestamp, so there is nothing to correlate against")
	}
	if tolerance <= 0 {
		tolerance = 5 * time.Minute
	}
	if limit <= 0 {
		limit = 20
	}

	from, to := dump.At.Add(-tolerance), dump.At.Add(tolerance)
	// The log is filtered by day because SAP keeps the date and the clock in
	// separate columns; the window itself is applied here, where both are known.
	entries, err := c.ApplicationLog(ctx, AppLogFilter{
		From:  from,
		To:    to,
		Limit: limit * 25,
	})
	if err != nil {
		return nil, err
	}

	matches := make([]LogMatch, 0, len(entries))
	for _, e := range entries {
		if e.At.IsZero() || e.At.Before(from) || e.At.After(to) {
			continue
		}
		match := LogMatch{Entry: e, Offset: dump.At.Sub(e.At)}
		match.Score, match.Why = rankLogAgainstDumpWithGraph(e, dump, stack, callees, match.Offset)
		matches = append(matches, match)
	}

	sort.Slice(matches, func(i, j int) bool {
		if matches[i].Score != matches[j].Score {
			return matches[i].Score > matches[j].Score
		}
		// Within a rung, nearer the dump first, measured on the absolute gap.
		return abs(matches[i].Offset) < abs(matches[j].Offset)
	})
	if len(matches) > limit {
		matches = matches[:limit]
	}
	return matches, nil
}

func rankLogAgainstDump(e AppLogEntry, dump Dump, stack []DumpFrame, offset time.Duration) (int, string) {
	return rankLogAgainstDumpWithGraph(e, dump, stack, nil, offset)
}

func rankLogAgainstDumpWithGraph(e AppLogEntry, dump Dump, stack []DumpFrame, callees map[string]string, offset time.Duration) (int, string) {
	when := "before"
	if offset < 0 {
		when = "after"
	}
	gap := abs(offset).Round(time.Second)

	if e.Program != "" && dump.Program != "" && equalFoldTrim(e.Program, dump.Program) {
		return scoreSameProgram, fmt.Sprintf("written by %s, the program that dumped — %s %s", e.Program, gap, when)
	}
	if frame, on := frameFor(stack, e.Program); on {
		return scoreOnStack, fmt.Sprintf("written by %s, frame %d of the dump stack (%s) — %s %s",
			e.Program, frame.Position, orUnknown(frame.Name), gap, when)
	}
	if caller, called := callees[trimUpper(e.Program)]; called {
		return scoreCalledByStack, fmt.Sprintf("written by %s, called from %s on the dump stack — %s %s",
			e.Program, caller, gap, when)
	}
	if e.User != "" && dump.User != "" && equalFoldTrim(e.User, dump.User) {
		if offset >= 0 {
			return scoreSameUserBefore, fmt.Sprintf("same user, %s before", gap)
		}
		return scoreSameUserAfter, fmt.Sprintf("same user, %s after — likely error handling, not cause", gap)
	}
	return scoreInWindow, fmt.Sprintf("only within the window, %s %s", gap, when)
}

func abs(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}

func equalFoldTrim(a, b string) bool {
	return len(a) > 0 && len(b) > 0 &&
		trimUpper(a) == trimUpper(b)
}

func trimUpper(s string) string {
	return strings.ToUpper(strings.TrimSpace(s))
}

// frameFor finds the stack frame a log entry's program belongs to.
func frameFor(stack []DumpFrame, program string) (DumpFrame, bool) {
	if strings.TrimSpace(program) == "" {
		return DumpFrame{}, false
	}
	want := trimUpper(program)
	for _, frame := range stack {
		if trimUpper(frame.Program) == want {
			return frame, true
		}
	}
	return DumpFrame{}, false
}

// scoreCalledByStack: written by something a stack frame calls. Weaker than
// being on the stack — the callee has already returned, so it is not on the
// path at the moment of failure — but it is where a bad value is most often
// prepared, and it is still structural rather than a coincidence of timing.
const scoreCalledByStack = 60

// CalleesUnavailable reports whether this system offers no way to ask what a
// program calls, which makes the graph rung of the ranking dead. A caller that
// prints the ladder should say so rather than show a rung that cannot fire.
//
// The probe is a one-row read of CROSS rather than a call on a real object,
// because the two ways this can be unavailable are both properties of the
// query and not of the object: free SQL blocked by this server's own safety
// settings, or a user without read authorisation on the cross-reference
// tables. Either way one row is enough to tell, and an object that happens to
// call nothing does not look like a broken system.
func (c *Client) CalleesUnavailable(ctx context.Context) bool {
	_, err := c.RunQuery(ctx, "SELECT INCLUDE FROM CROSS", 1)
	return err != nil
}

// callees walks one level down from each program on the stack and returns the
// names of what they call.
//
// One level, not five: depth buys breadth, and breadth here is a liability.
// A graph deep enough to reach everything makes "this is called somewhere below
// the stack" true of most of the system, which would promote noise into a
// structural-looking rung and quietly wreck the ranking.
//
// The source is Callees, over the CROSS and WBCROSSGT cross-reference tables.
// That is what the note here used to say would revive this rung, and it is now
// what feeds it. Only rows that are an invocation are taken: a class this frame
// merely names as a type never ran, so nothing it ever wrote can be evidence of
// anything, and letting types in would put most of DDIC on the rung.
//
// A frame widens to its object. The tables are keyed by include, so a METHOD
// frame is answered with everything its class reaches rather than everything
// that one method reaches — except for a FUNCTION frame, where the module has
// an include of its own and the answer stays narrow. The widening costs
// precision on the rung and cannot cost correctness: a caller that never ran
// only fails to match a log entry.
func (c *Client) calleesOfStack(ctx context.Context, stack []DumpFrame) map[string]string {
	out := map[string]string{}
	asked := map[string]bool{}
	for _, frame := range stack {
		// The whole frame, not just its program: a FUNCTION frame names its
		// module, and the module's own include is a far narrower answer than
		// its group's.
		var uri string
		if unit, ok := unitForFrame(frame); ok {
			uri = unit.URI
		}
		if uri == "" || asked[uri] {
			// The same class holds several frames of a normal stack, and its
			// callee list does not change between them.
			continue
		}
		asked[uri] = true
		// The gaps are deliberately not carried out of here. This rung is
		// evidence-matching, and the comment above says why a missing row
		// cannot cost correctness: a caller that never ran only fails to
		// match a log entry. A gap loses evidence, it cannot invent any.
		callees, _, err := c.Callees(ctx, uri)
		if err != nil {
			// A program whose references cannot be read simply contributes
			// nothing; CalleesUnavailable is what says whether that is true of
			// the whole system.
			continue
		}
		for _, callee := range callees {
			if !callee.Calls {
				continue
			}
			name := trimUpper(callee.Name)
			if name == "" {
				continue
			}
			if _, seen := out[name]; !seen {
				out[name] = frame.Program
			}
		}
	}
	return out
}

// programURI is the ADT URI of a program named in a dump stack.
//
// It unwrapped class pools and sent everything else to /programs/programs,
// which is wrong for function groups: SAPLSBAL_DB is not addressable there, and
// the 404 that comes back is indistinguishable from "this program calls
// nothing". unitForFrame in dumpimpact.go does the whole mapping — class and
// interface pools, function groups, function modules, programs — so this defers
// to it rather than keeping a second, thinner copy of the same knowledge.
func programURI(program string) string {
	unit, ok := unitForFrame(DumpFrame{Program: program})
	if !ok {
		return ""
	}
	return unit.URI
}
