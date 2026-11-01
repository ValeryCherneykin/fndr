# fndr

A search-first file manager for the terminal, written in Go.

You don't walk the tree, you type and the file is found. Filters stack like
cards (`.go` + `size>1MB` + `dirs`), and renaming, deleting and creating files
is just editing a text buffer with Vim keys.

> **Status:** early development. Nothing usable yet; follow along.

## Build

Requires Go 1.26.5 or newer.

```sh
make build        # bin/fndr
./bin/fndr -version
```

Run `make` to list all developer commands.

## License

[MIT](LICENSE)
