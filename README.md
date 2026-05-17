# ipgeo

`ipgeo` resolves IPv4 and IPv6 addresses to geographic and network information.

## CLI

The `ipgeo` command reads IP addresses from arguments, files, standard input, or an interactive prompt, then annotates each address with the first matching configured source.

### Install

#### Download prebuilt binaries

Download an archive from [GitHub Releases](https://github.com/kibaamor/ipgeo/releases/latest).

Optionally, download `checksums.txt` and verify the archive:

```bash
cd path/to/downloads
sha256sum -c checksums.txt --ignore-missing
```

Extract the archive and place `ipgeo` in your `PATH`.

#### Install from source

```bash
go install github.com/kibaamor/ipgeo/cmd/ipgeo@latest
```

### Usage

```bash
ipgeo 1.1.1.1
```

Examples:

```bash
# Resolve IPs passed as arguments.
ipgeo 1.1.1.1 8.8.8.8

# Annotate IPs from a log stream.
ipgeo < input.log

# Read from and write to files.
ipgeo --input input.log --output output.log

# Query only one configured source.
ipgeo --source GeoLite2 1.1.1.1

# Emit one JSON object per matched IP.
ipgeo --json < input.log

# Show version, config, and source file status.
ipgeo info

# Refresh configured source database files.
ipgeo update

# Upgrade the ipgeo CLI binary from GitHub Releases.
ipgeo upgrade
```

Useful flags: `-j/--json`, `-s/--source`, `-i/--input`, `-o/--output`, `-h/--help`.

### Configuration

On first run, `ipgeo` writes a default config to `$IPGEO_HOME/config.yaml`; if `IPGEO_HOME` is unset, it uses `~/.config/ipgeo/config.yaml`.

The default config includes these sources:

- `ip2region` (`xdb`) with separate IPv4 and IPv6 files.
- `GeoLite2` (`mmdb`) with a companion ASN database.
- `DBIPCityLite` (`mmdb`).

Source files are stored under `IPGEO_HOME`. Relative filenames are resolved under `IPGEO_HOME`, and paths that resolve outside that directory are rejected.

Config files can use the public schema at [`cmd/ipgeo/doc/config.schema.json`](./cmd/ipgeo/doc/config.schema.json).

## Go Library

The Go library composes one or more geolocation sources and queries them in order.

### Install

```bash
go get github.com/kibaamor/ipgeo
```

### Usage

Open a client with one or more source options, then call `Lookup`.

```go
package main

import (
	"fmt"
	"net/netip"

	"github.com/kibaamor/ipgeo"
)

func main() {
	client, err := ipgeo.Open(
		ipgeo.WithMMDB("GeoLite2", "GeoLite2-City.mmdb", "GeoLite2-ASN.mmdb"),
		ipgeo.WithCache(1024, 0),
	)
	if err != nil {
		panic(err)
	}
	defer client.Close()

	result, err := client.Lookup(netip.MustParseAddr("1.1.1.1"))
	if err != nil {
		panic(err)
	}
	if result == nil {
		fmt.Println("not found")
		return
	}

	fmt.Println(result)
}
```

Supported built-in source options:

- `WithMMDB` for MaxMind DB files, with an optional companion MMDB.
- `WithIPDB` for IPIP.net IPDB files.
- `WithXDB` for ip2region XDB files.
- `WithIP2Location` for IP2Location BIN files.
- `WithSource` for custom implementations.

Lookup methods:

- `Lookup` queries sources in order and returns the first result.
- `LookupAll` returns every result found.
- `LookupFrom` queries one named source.

## Issues

Bug reports and feature suggestions are welcome in [GitHub Issues](https://github.com/kibaamor/ipgeo/issues).

When reporting a CLI bug, please include the input that reproduces it, the expected output, the actual output, and the `ipgeo info` output.

## License

Apache License 2.0. See [LICENSE](./LICENSE).
