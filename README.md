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

### Interactive

`-i` on a terminal searches incrementally: results are redrawn on every
keystroke, `↑`/`↓` move the selection, `enter` accepts it and `esc` quits.
`ctrl-w` deletes a word, `ctrl-u` clears the line.

The redraw goes to stderr and the accepted result to stdout, so the selection
can be captured:

```sh
read -r town lat lon <<<"$(typetown query -i -home US)"
```

When stdin is not a terminal there are no keystrokes to react to, so `-i` falls
back to reading whole lines.

## Use as a library

Build the index with the command, then embed the query side. There is no server
to run and no network hop: opening an index memory-maps it, and a lookup is tens
of microseconds in your own process.

```sh
go get github.com/jasiek/typetown
```

```go
ix, err := typetown.Open("/srv/typetown/index")
if err != nil {
	log.Fatal(err)
}
defer ix.Close()

places, err := ix.Search("krak", typetown.SearchOptions{Limit: 5, Home: "PL"})
// places[0] == {Name: "Kraków", Region: "Lesser Poland", Country: "Poland",
//               CC: "PL", Lat: 50.06143, Lon: 19.93658, ...}
```

`Search` is safe for concurrent use. `Close` is not, and must not race with it.

### A ready-to-attach endpoint

`Handler` returns an `http.Handler` that answers lookups as JSON. It handles one
route and does not care where you mount it:

```go
http.Handle("/places", typetown.Handler(ix,
	typetown.WithHome("PL"),                    // bias when the request says nothing
	typetown.WithHomeHeader("CF-IPCountry"),    // ...or take it from a trusted proxy
	typetown.WithLimit(5, 20),                  // default and hard maximum
	typetown.WithCORS("https://example.com"),   // omit for backend-only callers
))
```

The caller's position can be passed too, and is worth much more than the country:

```
GET /places?q=springfield&lat=39.80&lon=-89.64
```

`lat` and `lon` must be given together and must be in range, or the request is
400. They combine with the country hint rather than replacing it, so sending
both is safe. Coordinates in a query string end up in access logs and cache
keys, so round them before sending — two decimal places is about a kilometre,
which is all the ranking can use.

```
GET /places?q=lond&limit=3&home=US

{"query":"lond","results":[
  {"id":2643743,"name":"London","region":"England","country":"United Kingdom",
   "cc":"GB","lat":51.50853,"lon":-0.12573,"population":8961989,"kind":"PPLC","score":8.2},
  {"id":4517009,"name":"London","region":"Ohio","country":"United States", ...},
  {"id":4298960,"name":"London","region":"Kentucky","country":"United States", ...}]}
```

A missing `q` is 400 and a non-GET method is 405, but a query that matches
nothing is 200 with an empty list — finding no such town is an answer, not an
error. `limit` is clamped to the configured maximum, so one request cannot be
made arbitrarily expensive. Successful responses carry `Cache-Control`, since an
index is immutable once built.

Set `WithHomeHeader` only where a trusted proxy sets the header: otherwise a
client chooses its own bias. The consequence is mild — results come back in a
different order — but it is still input from the network.

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

**Prominence** is population, plus the feature code at a quarter weight as a
tiebreak.

Population is recorded for only 9% of populated places, which looks like a
reason not to lead with it — and was, until it was measured. That statistic
describes the *dataset*, not the *queries*: the places people look up are the
ones that have a population figure, and the ones that do not are the tail nobody
searches for. Among the towns in the benchmark, population is present 88% of the
time.

The feature code — GeoNames' own statement of administrative rank — is what
separates places whose population is unrecorded. It is deliberately scaled down:
at full strength a national capital outranked a place with 100,000 more
residents, which measured worse in every country tested.

A third signal was tried and removed: how many writing systems name a place,
which is a better proxy for international notability than a raw count of
alternate names. It lowered accuracy in all 24 countries in the benchmark,
including the ones whose own script is not Latin.

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

### Memory

Index files are mapped read-only (`PROT_READ`, `MAP_SHARED`) rather than read
onto the heap. Replicas sharing a volume then share one copy of the index in the
host page cache no matter how many run, those pages are reclaimable under memory
pressure instead of triggering an OOM kill, and the Go collector never has to
size a heap around hundreds of megabytes it can never free. A single query
process holds about 7 MB resident against a 266 MB index.

The trade is that a mapping is a live view, not a snapshot: **replace an index by
building into a new directory and swapping, never by overwriting files a reader
may still hold open.** `OpenOptions{NoMmap: true}` reads instead, for callers who
would rather have the snapshot.

## The index on disk

Seven files. A filled diamond means one structure contains the other; a dashed
arrow is a pointer — a number stored in one file that addresses another. Every
hop below is one of those numbers, so a query resolves by arithmetic rather than
by scanning.

```mermaid
classDiagram
    direction TB

    class NamesFST {
        <<names.fst — 5,532,766 entries>>
        +bytes key
        +uint64 value
    }
    class HotFST {
        <<hot.fst — 6,489 entries>>
        +bytes key
        +uint64 value
    }
    class PostingList {
        <<postings.bin — 5,532,766 lists>>
        +uvarint count
    }
    class HotList {
        <<hot.bin — 6,489 lists, count max 256>>
        +uvarint count
    }
    class Posting {
        <<9,300,117 in total>>
        +uvarint ordinal
        +varint score
    }
    class RecordsIdx {
        <<records.idx — 5,165,451 slots>>
        +uint32LE offset
    }
    class Record {
        <<records.bin — 5,165,450 records>>
        +uvarint id
        +varint lat
        +varint lon
        +uvarint population
        +uvarint featureIdx
        +uvarint countryIdx
        +uvarint regionIdx
        +uvarint nameLen
        +bytes name
    }
    class FeatureCodes {
        <<manifest.featureCodes — 15>>
        +string code
    }
    class Countries {
        <<manifest.countries + countryNames — 248, parallel>>
        +string cc
        +string name
    }
    class Regions {
        <<manifest.regions — 3,752, slot 0 = none>>
        +string name
    }

    PostingList *-- "count" Posting : contains
    HotList *-- "count" Posting : contains

    NamesFST ..> PostingList : value = byte offset
    HotFST ..> HotList : value = byte offset
    Posting ..> RecordsIdx : ordinal = slot
    RecordsIdx ..> Record : offset = first byte
    Record ..> FeatureCodes : featureIdx
    Record ..> Countries : countryIdx
    Record ..> Regions : regionIdx
```

`names.fst` is walked for every query. `hot.fst` short-circuits the prefixes with
too many keys beneath them to walk on a keystroke; both paths converge on record
ordinals. Repeated strings — country, region, feature code — live once in
`manifest.json` and are referenced by position, so "United Kingdom" is not
written 250,000 times.

`records.idx` exists because a record is variable-length: eight of its nine
fields are varints, so there is no arithmetic path from an ordinal to a byte
offset. Its extra trailing slot holds the file's length, which lets record `i`
span `[offset[i], offset[i+1])` with no special case for the last one.

## Layout

```
typetown.go         the library: Open, Search, Result
http.go             Handler, and the options that configure it
cmd/typetown/       thin main(): wires stdio into the CLI and exits
internal/cli/       flag parsing, subcommand registry, usage
internal/geonames/  parsing the dumps and folding names
internal/index/     the on-disk format, the builder, and search
```

Only the root package is public. Everything the library does not need to expose
stays under `internal/`, so the on-disk format and the builder can change without
breaking anyone.

## Development

```sh
make test   # go test ./...
make lint   # go vet ./...
make tidy   # go mod tidy
```
