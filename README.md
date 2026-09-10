# typetown

A command-line tool that builds a finite state transducer based autocomplete
index over the populated places of the world, and queries it.

Given a prefix of a town's name it returns the locality — town, first-order
division, country — and its coordinates, so the result can be handed to
something that does proximity work.

```
$ typetown query -home US "san fr"
  San Francisco, California, United States         37.7749, -122.4194  pop 873965, PPLA2
```

## Requirements

Go 1.27 (see `.tool-versions` for the exact toolchain).

## Getting the data

```sh
make sources    # ~625 MB from download.geonames.org, fetched once
```

Four inputs are used:

| file | what it provides |
|---|---|
| `allCountries.zip` | every place; 13.5M rows, of which 5.2M are populated places |
| `alternateNamesV2.zip` | names in other languages, **with the flags that make them usable** |
| `admin1CodesASCII.txt` | `GB.ENG` → `England` |
| `countryInfo.txt` | `GB` → `United Kingdom` |

`alternateNamesV2.zip` is worth its size: the equivalent column inside
`allCountries.txt` is a lossy copy with the language codes and the
`isColloquial` / `isHistoric` / `isPreferredName` flags stripped out. Without
them, "New York Van Java" is an alternate name of Jakarta and outranks New York
City for the query `new y`.

## Build

```sh
make build                  # -> bin/typetown
typetown build              # -> ./index, from ./sources
typetown build -countries GB -out index-gb        # a fast subset for development
typetown build -min-population 1000               # smaller index, fewer hamlets
```

## Query

```sh
typetown query lond                    # one shot
typetown query -home US -limit 5 york  # bias toward a country
typetown query -json edinburgh         # machine-readable
typetown query -i -home US             # interactive
```

## Benchmark

`bench` measures latency, and accuracy too when the query set says what each
query should return. A query set is one case per line:

```
<query>
<query>	<expected geonameid>
<query>	<expected geonameid>	<weight>
```

```sh
typetown bench                                     # built-in sample, latency only
typetown bench -queries set.tsv -home US           # latency and accuracy
typetown bench -queries set.tsv -prefixes 3,4,5    # accuracy as a user types
```

## How it ranks

Every match is scored `prominence − classPenalty`, both fixed at build time so
posting lists can be written pre-sorted and a query is a merge rather than a
scan.

**Prominence** combines three signals, in descending order of how often they are
actually present:

- **script diversity** — how many writing systems name the place. Present for
  34% of places, and unlike a raw count of alternate names it is not fooled by
  transliteration variance.
- **feature code** — GeoNames' own statement of administrative rank: a national
  capital outranks a county seat outranks a village.
- **population** — the obvious signal, but it is zero for about 91% of populated
  places, so it cannot carry the ranking alone.

**Class penalty** demotes matches reached through a lesser name — an official
alternate, an ordinary alternate, a historic name. The penalties are bounded on
purpose: a hard tier would bury Mumbai for the query `bombay`, while no penalty
at all lets a large city hijack an unrelated prefix.

Two adjustments happen at query time, because they depend on the caller rather
than the data: an exact-match bonus, and an optional bias toward a home country.
They are applied as a bounded re-rank over a pool of candidates pulled from the
merge.

### Hot prefixes

A short prefix has an enormous number of keys beneath it — `p` covers 333,538 —
which is far too many to walk on every keystroke. Truncating the walk is not an
answer, because the FST yields keys alphabetically: a cap silently ranks by
spelling, and `p` returns Pa Sang, Thailand instead of Paris.

Nearly all of that cost sits in a few thousand prefixes, so the build
precomputes a ranked shortlist for every prefix with at least 500 keys beneath
it — about 6,500 of them, 8 MB — and scans everything else exhaustively, which
is cheap precisely because it is not hot.

## Layout

```
cmd/typetown/       thin main(): wires stdio into the CLI and exits
internal/cli/       flag parsing, subcommand registry, usage
internal/geonames/  parsing the dumps; name folding; script counting
internal/index/     the on-disk format, the builder, and search
```

An index directory holds four files: `manifest.json` (counts and the string
tables records refer to by number), `names.fst` (the transducer, mapping a
folded name to a postings offset), `postings.bin` (per key, the matching records
in descending score order) and `records.bin` (the packed places).

## Development

```sh
make test   # go test ./...
make lint   # go vet ./...
make tidy   # go mod tidy
```
