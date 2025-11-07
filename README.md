# CowFS - Copy on Write FileSystem

[![MIT License](https://img.shields.io/badge/license-MIT-blue.svg)](https://github.com/absfs/cowfs/blob/master/LICENSE)

The `cowfs` package implements a Copy-on-Write FileSystem that wraps two `absfs.Filer` implementations. It reads from a primary read-only filesystem and directs all writes and modifications to a secondary writable filesystem, leaving the primary unchanged.

## Features

- **Non-destructive writes**: Primary filesystem remains unmodified
- **Transparent reads**: Automatically selects primary or secondary based on modification state
- **Full write support**: All modifications go to secondary filesystem

## Install

```bash
go get github.com/absfs/cowfs
```

## Example Usage

```go
package main

import (
    "os"

    "github.com/absfs/cowfs"
    "github.com/absfs/memfs"
    "github.com/absfs/osfs"
)

func main() {
    // Create primary read-only filesystem
    primary, _ := osfs.NewFS()

    // Create secondary writable filesystem for modifications
    secondary, _ := memfs.NewFS()

    // Create copy-on-write filesystem
    fs := cowfs.New(primary, secondary)

    // Reads come from primary
    f, _ := fs.OpenFile("/config/app.conf", os.O_RDONLY, 0)
    defer f.Close()

    // Writes go to secondary, primary unchanged
    w, _ := fs.OpenFile("/config/app.conf", os.O_WRONLY, 0644)
    w.WriteString("modified content")
    w.Close()

    // Subsequent reads come from secondary
    // (since file was modified)
}
```

## Use Cases

- Testing with production data without modification risk
- Implementing ephemeral filesystem overlays
- Creating sandboxed environments

## absfs

Check out the [`absfs`](https://github.com/absfs/absfs) repo for more information about the abstract filesystem interface and features like filesystem composition.

## LICENSE

This project is governed by the MIT License. See [LICENSE](https://github.com/absfs/cowfs/blob/master/LICENSE)
