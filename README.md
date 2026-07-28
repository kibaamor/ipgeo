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
```

Useful flags: `-j/--json`, `-s/--source`, `-i/--input`, `-o/--output`, `-h/--help`.

### Configuration

On first run, `ipgeo` writes a default config to `$IPGEO_HOME/config.yaml`; if `IPGEO_HOME` is unset, it uses `~/.config/ipgeo/config.yaml`.

The `updater:` configuration block (used by the removed `ipgeo upgrade` command) is no longer supported. If your existing config contains it, delete the block — `ipgeo` now rejects unknown fields.

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

Build a source with `MMDB`, `IPDB`, `XDB`, `IP2Location`, or `Wrap`, decorate
it with `Singleflight` and/or `Cache`, then open a client.

```go
package main

import (
	"context"
	"errors"
	"fmt"
	"net/netip"

	"github.com/kibaamor/ipgeo"
)

func main() {
	client, err := ipgeo.Open(
		ipgeo.MMDB("GeoLite2", "GeoLite2-City.mmdb", "GeoLite2-ASN.mmdb").
			Decorate(ipgeo.Singleflight()).
			Decorate(ipgeo.Cache(1024, 0, 0)),
	)
	if err != nil {
		panic(err)
	}
	defer client.Close()

	result, err := client.Lookup(context.Background(), netip.MustParseAddr("1.1.1.1"))
	if err != nil {
		if errors.Is(err, ipgeo.ErrNotFound) {
			fmt.Println("not found")
			return
		}
		panic(err)
	}

	fmt.Println(result)
}
```

Supported built-in source constructors:

- `MMDB` for MaxMind DB files, with an optional companion MMDB.
- `IPDB` for IPIP.net IPDB files.
- `XDB` for ip2region XDB files.
- `IP2Location` for IP2Location BIN files.
- `Wrap` for custom `Source` implementations.

Built-in decorators (applied in call order; first added is innermost):

- `Singleflight` deduplicates concurrent lookups for the same address.
- `Cache` caches results (sliding TTL) and optionally errors (fixed TTL).

Lookup methods (each accepts a `context.Context` for cancellation/timeout):

- `Lookup` queries sources in order and returns the first result.
- `LookupAll` returns every result found.
- `LookupFrom` queries one named source.

When no source has a matching record, `Lookup`, `LookupAll`, and `LookupFrom`
return `ErrNotFound`; test it with `errors.Is`.

A `Client` is safe for concurrent use. `Close` is idempotent. Common failures
are exported as sentinel errors (`ErrNoSources`, `ErrDuplicateSource`,
`ErrSourceNotConfigured`, `ErrNotFound`).

## Issues

Bug reports and feature suggestions are welcome in [GitHub Issues](https://github.com/kibaamor/ipgeo/issues).

When reporting a CLI bug, please include the input that reproduces it, the expected output, the actual output, and the `ipgeo info` output.

## License

Apache License 2.0. See [LICENSE](./LICENSE).
