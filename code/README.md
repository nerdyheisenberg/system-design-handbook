# Runnable code

Working Go implementations of every algorithm in the book. They compile, they
have tests, and each one prints something instructive when you run it.

**No external dependencies.** Everything here uses only the standard library, so
`go test ./...` works offline and will keep working.

```bash
go test ./...              # run every test
go test -v ./bloom_filter  # one package, verbose
go test -bench=. ./wal     # benchmarks
go run ./hyperloglog       # run a demo
go vet ./...
```

Requires Go 1.23 or later.

---

## What each package demonstrates

Each is a `package main` containing the implementation, a test file, and a
`main()` that prints a demonstration.

### Caching — Chapter 11

| Package | Implements | The point |
| --- | --- | --- |
| `lru_cache/` | O(1) LRU: map + doubly-linked list | Why a list and not a timestamp scan |
| `lfu_cache/` | O(1) LFU with frequency buckets | Eviction stays O(1) via bucket adjacency |

### Probabilistic structures — Chapter 23

| Package | Implements | The point |
| --- | --- | --- |
| `bloom_filter/` | Bloom filter with the sizing formulas | 10M items at 1% FP in 11 MB vs 650 MB exact |
| `count_min_sketch/` | Frequency estimation | Never under-counts; heavy hitters are near-exact |
| `hyperloglog/` | Cardinality estimation | 10M distinct in 16 KB at ~1% error, and mergeable |
| `consistent_hashing/` | Hash ring with virtual nodes | Adding a node moves ~1/N of keys, not ~80% |

### Storage engines — Chapter 6

| Package | Implements | The point |
| --- | --- | --- |
| `wal/` | Write-ahead log, CRC, crash recovery | Torn tail writes are expected and survivable |
| `lsm_memtable/` | Skip-list memtable → SSTable | Write path is all sequential I/O |
| `b_tree/` | B+tree with splits and leaf chaining | Range scans walk leaves, not the tree |
| `merkle_tree/` | Merkle tree for anti-entropy | 4 differences among 100k keys in 81 comparisons |

### Resilience — Chapter 16

| Package | Implements | The point |
| --- | --- | --- |
| `circuit_breaker/` | Three-state breaker with probe capping | A failed probe reopens immediately |
| `retry_backoff_jitter/` | Four jitter strategies | Quantifies the thundering herd |
| `bulkhead_semaphore/` | Concurrency isolation | One slow dependency cannot starve the rest |
| `rate_limiter/` | Token, leaky, fixed and sliding window | Demonstrates the fixed-window 2× boundary flaw |

### Distributed systems — Chapters 9, 21, 23

| Package | Implements | The point |
| --- | --- | --- |
| `vector_clock/` | Vector clocks, sibling resolution | Detects *concurrent*, which timestamps cannot |
| `raft_election_sim/` | Raft leader election | Two leaders in one term is impossible, by test |
| `snowflake_id/` | 64-bit time-sortable IDs | Clock skew is refused, not papered over |
| `geohash/` | Encode, decode, neighbours | ⚠️ Proves the boundary problem is real |

### Data and services — Chapters 10, 12, 13, 15

| Package | Implements | The point |
| --- | --- | --- |
| `external_merge_sort/` | k-way merge with bounded memory | 1M records sorted holding 10k at a time |
| `worker_pool_pipeline/` | Staged pipeline, bounded channels | Backpressure measured, not asserted |
| `grpc_example/` | Protobuf wire format + RPC server/client | Every byte of the encoding, by hand |
| `outbox_pattern/` | Transactional outbox | Solves the dual-write problem |
| `idempotency_keys/` | Full idempotency state machine | Includes the in-progress state most omit |

---

## Notes

**These are teaching implementations.** They are correct and tested, but they
optimise for legibility over the last 20% of performance, and they omit
production concerns like metrics and structured logging. Use `redis/go-redis`,
`hashicorp/raft` and `google.golang.org/grpc` in production — but read these
first, because knowing what those libraries are doing is the point.

**The race detector** needs cgo and a C toolchain. If you have gcc:

```bash
CGO_ENABLED=1 go test -race ./...
```

The concurrent packages (`bulkhead_semaphore`, `circuit_breaker`,
`rate_limiter`, `worker_pool_pipeline`, `snowflake_id`, `outbox_pattern`,
`idempotency_keys`) have tests written specifically to exercise it.

**Configuration examples** — Docker, Kubernetes, nginx, Prometheus, Kafka and
Postgres — live in [`../config/`](../config/), annotated in the same style.
