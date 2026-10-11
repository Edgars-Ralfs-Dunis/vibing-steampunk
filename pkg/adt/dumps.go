package adt

import (
	"context"
	"encoding/xml"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
)

// Runtime errors — ST22 — come out of ADT as an Atom feed, and the useful parts
// are structured rather than prose: the error type and the program that died
// are Atom categories, the user is the entry author, and the timestamp is the
// publication date. So listing and grouping need no HTML parsing at all. The
// escaped markup in the summary is the detail view, and it is the brittle part:
// it is kept on the Dump and read only where nothing better exists
// (DumpDetailFromSummary, on a release with no detail resource).

// Dump is one runtime error.
type Dump struct {
	ID        string    `json:"id"`
	At        time.Time `json:"at"`
	User      string    `json:"user,omitempty"`
	ErrorType string    `json:"errorType,omitempty"`
	// Program is the terminated program: a report, or the class pool of the
	// class that died.
	Program string `json:"program,omitempty"`
	Message string `json:"message,omitempty"`
	// Summary is the entry's HTML summary, kept for DumpDetailFromSummary.
	Summary string `json:"-"`
}

// DumpFilter narrows a dump search.
type DumpFilter struct {
	// From and To bound the feed. The resource pages on these, so a bounded
	// search is much cheaper than filtering afterwards.
	From, To time.Time
	// ErrorType and Program filter what comes back, matched case-insensitively.
	ErrorType, Program string
	// User filters on the entry's author.
	User string
	// Limit caps the dumps returned; 100 when unset.
	Limit int
}

const dumpStampLayout = "20060102150405"

// Dumps reads runtime errors, newest first.
func (c *Client) Dumps(ctx context.Context, filter DumpFilter) ([]Dump, error) {
	q := url.Values{}
	if !filter.From.IsZero() {
		q.Set("from", filter.From.UTC().Format(dumpStampLayout))
	}
	if !filter.To.IsZero() {
		q.Set("to", filter.To.UTC().Format(dumpStampLayout))
	}
	path := "/sap/bc/adt/runtime/dumps"
	if len(q) > 0 {
		path += "?" + q.Encode()
	}

	res, err := c.transport.Request(ctx, path, &RequestOptions{Method: "GET", Accept: acceptAny})
	if err != nil {
		return nil, fmt.Errorf("reading runtime errors: %w", err)
	}
	dumps, err := parseDumpFeed(res.Body)
	if err != nil {
		return nil, err
	}

	limit := filter.Limit
	if limit <= 0 {
		limit = 100
	}
	kept := make([]Dump, 0, limit)
	for _, d := range dumps {
		if !filter.matches(d) {
			continue
		}
		kept = append(kept, d)
		if len(kept) >= limit {
			break
		}
	}
	return kept, nil
}

func (f DumpFilter) matches(d Dump) bool {
	eq := func(want, got string) bool {
		return want == "" || strings.EqualFold(strings.TrimSpace(want), got)
	}
	return eq(f.ErrorType, d.ErrorType) && eq(f.Program, d.Program) && eq(f.User, d.User)
}

// parseDumpFeed reads the Atom feed. The categories are what matter: SAP labels
// one "ABAP runtime error" and the other "Terminated ABAP program", and going by
// the label rather than by position means a release that adds a third category
// does not shift the meaning of the first two.
func parseDumpFeed(body []byte) ([]Dump, error) {
	var feed struct {
		Entries []struct {
			ID        string `xml:"id"`
			Title     string `xml:"title"`
			Summary   string `xml:"summary"`
			Published string `xml:"published"`
			Author    struct {
				Name string `xml:"name"`
			} `xml:"author"`
			Categories []struct {
				Term  string `xml:"term,attr"`
				Label string `xml:"label,attr"`
			} `xml:"category"`
		} `xml:"entry"`
	}
	if err := xml.Unmarshal(body, &feed); err != nil {
		return nil, fmt.Errorf("the runtime error feed did not parse: %w", err)
	}

	dumps := make([]Dump, 0, len(feed.Entries))
	for _, e := range feed.Entries {
		d := Dump{
			ID:      strings.TrimSpace(e.ID),
			User:    strings.ToUpper(strings.TrimSpace(e.Author.Name)),
			Message: strings.TrimSpace(e.Title),
			Summary: e.Summary,
		}
		if at, err := time.Parse(time.RFC3339, strings.TrimSpace(e.Published)); err == nil {
			d.At = at
		}
		for _, cat := range e.Categories {
			switch {
			case strings.EqualFold(cat.Label, "ABAP runtime error"):
				d.ErrorType = strings.TrimSpace(cat.Term)
			case strings.EqualFold(cat.Label, "Terminated ABAP program"):
				d.Program = strings.TrimSpace(cat.Term)
			}
		}
		dumps = append(dumps, d)
	}
	return dumps, nil
}

// DumpGroup is dumps that are the same failure rather than the same moment.
type DumpGroup struct {
	ErrorType string    `json:"errorType"`
	Program   string    `json:"program"`
	Count     int       `json:"count"`
	First     time.Time `json:"first"`
	Last      time.Time `json:"last"`
	Users     []string  `json:"users,omitempty"`
}

// GroupDumps collapses dumps by what failed rather than when.
//
// This is the question people actually have — is this new, and how often does
// it happen — and grouping by error type and program answers it structurally.
// Grouping by "the same afternoon" would not: a busy hour makes unrelated
// failures look like one incident.
func GroupDumps(dumps []Dump) []DumpGroup {
	type key struct{ errorType, program string }
	index := map[key]*DumpGroup{}
	users := map[key]map[string]bool{}

	for _, d := range dumps {
		k := key{d.ErrorType, d.Program}
		group, seen := index[k]
		if !seen {
			group = &DumpGroup{ErrorType: d.ErrorType, Program: d.Program, First: d.At, Last: d.At}
			index[k] = group
			users[k] = map[string]bool{}
		}
		group.Count++
		if !d.At.IsZero() {
			if group.First.IsZero() || d.At.Before(group.First) {
				group.First = d.At
			}
			if d.At.After(group.Last) {
				group.Last = d.At
			}
		}
		if d.User != "" {
			users[k][d.User] = true
		}
	}

	out := make([]DumpGroup, 0, len(index))
	for k, group := range index {
		for user := range users[k] {
			group.Users = append(group.Users, user)
		}
		sort.Strings(group.Users)
		out = append(out, *group)
	}
	// Most frequent first; ties broken by most recent, so a group that is
	// happening now outranks one that stopped last month.
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Last.After(out[j].Last)
	})
	return out
}

// DumpTimeFromID recovers the moment a dump was written from its own id.
//
// An ST22 id begins with the timestamp — 20260824012009 — followed by the
// instance, the user, the client and a counter. That matters because a caller
// who quotes a full id is addressing a dump the feed may no longer carry, and
// the alternative to reading the id is a Dump with no time in it at all, which
// then fails correlation with "this dump carries no timestamp". The information
// was never missing; it was in the caller's hand the whole time.
//
// Returns the zero time when the id does not start with a timestamp, so a
// caller can tell recovery from invention.
func DumpTimeFromID(id string) time.Time {
	tail := id
	if i := strings.LastIndex(tail, "/"); i >= 0 {
		tail = tail[i+1:]
	}
	if len(tail) < 14 {
		return time.Time{}
	}
	stamp := tail[:14]
	for _, r := range stamp {
		if r < '0' || r > '9' {
			return time.Time{}
		}
	}
	// The stamp is the server's clock, not UTC: the feed's <published> for the
	// same dump is UTC and differs by the server's offset (2 h on a CEST
	// system). The reading comes back as is; DumpClockOffset converts it.
	at, err := time.Parse("20060102150405", stamp)
	if err != nil {
		return time.Time{}
	}
	return at.UTC()
}

// DumpClockOffset is how far the server's clock runs ahead of UTC, read off
// listed dumps: each id starts with the server-local stamp, each <published>
// is UTC. False when no listed dump shows both.
//
// ponytail: one offset for every date, so a dump from the other side of a
// summer-time change comes out an hour off; the system zone (TTZCU) and its
// rules are the upgrade.
func DumpClockOffset(dumps []Dump) (time.Duration, bool) {
	for _, d := range dumps {
		if local := DumpTimeFromID(d.ID); !local.IsZero() && !d.At.IsZero() {
			return local.Sub(d.At), true
		}
	}
	return 0, false
}
