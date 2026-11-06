// Package tui is the terminal interface of fndr, built on Bubble Tea.
//
// It is deliberately thin: crawling, indexing, matching and ranking live in
// package indexer, and this package only turns user input into queries and
// query results into text. Everything slow (crawling, searching) runs in
// Bubble Tea commands off the UI goroutine, so input never waits on the disk
// or on a search.
//
// Layout, top to bottom: the query line, a hairline, the result list, and one
// status bar. There are no boxes or borders around anything.
package tui
