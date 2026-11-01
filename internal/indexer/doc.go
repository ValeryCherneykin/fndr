// Package indexer builds and searches an in-memory index of the file system.
//
// The index is a flat []Entry rather than a tree of nodes: one contiguous
// allocation that a search scans front to back. That layout keeps the CPU
// cache warm and gives the garbage collector one slice to track instead of
// a million small objects, which is what makes per-keystroke search over
// hundreds of thousands of files feel instant.
//
// Paths are stored absolute, so a single index of the home directory serves
// both scopes: GLOBAL searches every entry, LOCAL is the same index narrowed
// to a directory prefix.
package indexer
