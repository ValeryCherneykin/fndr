// Package search executes queries over immutable filesystem index snapshots.
package search

import (
	"context"
	"runtime"
	"strings"
	"time"

	"github.com/ValeryCherneykin/fndr/internal/filter"
	"github.com/ValeryCherneykin/fndr/internal/indexer"
)

// MatchMode determines which fuzzy matches are eligible for ranking.
type MatchMode uint8

const (
	// Fuzzy accepts subsequences, as in the regular search view.
	Fuzzy MatchMode = iota
	// Contiguous also requires the text to appear in one piece, for Oil.
	Contiguous
)

// Request describes a search. Results do not depend on whether candidates
// from a previous search can be reused.
type Request struct {
	Query string         // as typed, including a possible trailing slash
	Under string         // clean absolute directory, or empty for the whole snapshot
	Tags  filter.Stack   // immutable for the lifetime of this request and its narrowing
	Boost map[string]int // read-only frecency weights, used only for ranking
	Mode  MatchMode
	Limit int // zero or less uses DefaultLimit

	TrackMatches bool       // retain candidate positions for subsequent typing
	Previous     *Narrowing // candidates from an earlier response, if any
}

// Response contains the ranked results and the count before applying the limit.
type Response struct {
	Results   []Result
	Total     int
	Narrowing *Narrowing // nil when retaining candidates offers no benefit
}

// HasText reports whether the query includes text to match. A trailing slash
// asks for directories and is not part of that text.
func (r Request) HasText() bool {
	text, _ := splitQuery(r.Query)
	return text != ""
}

func splitQuery(raw string) (text string, dirs bool) {
	return strings.CutSuffix(raw, "/")
}

// Run executes the request against snap, which must be immutable. All time
// filters and candidate reuse use the calendar day of the supplied now.
func (r Request) Run(ctx context.Context, snap []indexer.Entry, now time.Time) (Response, error) {
	text, dirs := splitQuery(r.Query)
	q := scanQuery{Text: text, Dirs: dirs, Under: r.Under, Boost: r.Boost, Limit: r.Limit}
	if len(r.Tags) > 0 {
		q.Keep = r.Tags.Keep(snap, r.Under, now)
	}
	if r.Mode == Contiguous && text != "" {
		keep, lower := q.Keep, strings.ToLower(text)
		q.Keep = func(e indexer.Entry) bool {
			return containsWhole(e.Path, r.Under, lower) && (keep == nil || keep(e))
		}
	}

	var key searchKey
	if r.TrackMatches {
		key = keyFor(snap, r, text, dirs, now)
		q.Within = r.Previous.within(key)
	}
	results, total, matched, err := scan(ctx, snap, q, runtime.GOMAXPROCS(0), r.TrackMatches)
	if err != nil {
		return Response{}, err
	}
	resp := Response{Results: results, Total: total}
	if matched != nil {
		resp.Narrowing = &Narrowing{key: key, matched: matched}
	}
	return resp, nil
}

// ContainsWhole reports whether the portion of path below under contains text,
// case insensitively. The caller selects the scope before checking containment.
func ContainsWhole(path, under, text string) bool {
	return containsWhole(path, under, strings.ToLower(text))
}

func containsWhole(path, under, lower string) bool {
	return strings.Contains(strings.ToLower(strings.TrimPrefix(path, under)), lower)
}
