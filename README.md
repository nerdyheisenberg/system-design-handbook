# System Design: From Absolute Zero to Expert

> A complete, self-contained book on how large software systems are actually built.
> Written for someone who has **never** built a backend, and taken all the way to the
> level expected of a Staff Engineer.

---

## Why this book exists

Most system design material has a hidden prerequisite: it assumes you already know what
a load balancer is, what "eventual consistency" means, and why anyone would ever want a
message queue. It hands you a bullet list — *"use consistent hashing to distribute keys"* —
without ever showing you the thing that breaks when you don't.

This book does the opposite. Every idea is introduced by first showing you **the problem
that made someone invent it**. Every acronym is expanded the first time it appears. Every
claim comes with a number you can check. Every chapter contains arithmetic worked out line
by line, not just a conclusion.

If you read this book front to back, you will be able to sit in a room with the engineers
who run Netflix, Uber or Stripe, understand the words they are saying, and disagree with
them for good reasons.

---

## The three rules of this book

**1. Nothing is assumed.** The first time a term appears it is defined in plain English.
If you hit a word you don't know, it's either defined right there or in the
[Glossary](./99_glossary_and_cheatsheets.md).

**2. Numbers, not adjectives.** "Fast" is meaningless. "A read from RAM takes 100
nanoseconds, a read from an SSD takes 100 microseconds — a factor of 1,000" is a fact you
can design with. Every performance claim in this book carries a number.

**3. Every choice has a cost.** There are no "best" technologies. There are only
technologies that are correct for a specific set of constraints. Every alternative in this
book gets an explicit *"choose this when…"* and *"never choose this when…"*.

---

## How every chapter is structured

Each chapter follows the same ten-part shape, so you always know where you are:

| Section | What it gives you |
| --- | --- |
| **What you'll learn** | The contract. Read this to decide if you can skip the chapter. |
| **Start from zero** | The idea explained with a real-world analogy, zero jargon. |
| **The mental model** | One diagram that the whole chapter hangs off. |
| **Deep dive** | How the thing actually works internally, mechanism by mechanism. |
| **Worked example** | Real arithmetic, shown step by step, with real numbers. |
| **Trade-offs** | A table of alternatives, each with "choose when" / "never when". |
| **How real companies do it** | Named systems at named scale. Not hypotheticals. |
| **Common mistakes** | The specific ways engineers get this wrong in production. |
| **Interview angle** | Questions you'll be asked, with weak vs. strong answers. |
| **Recap & test yourself** | Compression of the chapter, then questions to check yourself. |

---

## Table of contents

### Part 0 — Orientation

| # | Chapter | What it covers |
| --- | --- | --- |
| — | [How to use this book](./00_how_to_use_this_book.md) | Learning paths, notation, how to study |

### Part 1 — Foundations

*Everything else in the book depends on these five chapters. Do not skip them.*

| # | Chapter | What it covers |
| --- | --- | --- |
| 01 | [From Zero: Computers, Networks and the Web](./01_from_zero_computers_networks_web.md) | CPU/RAM/disk, processes and threads, sockets, what happens when you type a URL, why one machine isn't enough |
| 02 | [Scalability and Back-of-the-Envelope Estimation](./02_scalability_and_estimation.md) | Vertical vs horizontal scaling, powers of two, latency numbers, Little's Law, Amdahl's Law, the Universal Scalability Law, six worked estimations |
| 03 | [Reliability, Availability and Performance](./03_reliability_availability_performance.md) | The nines, MTBF/MTTR, redundancy math, SLI/SLO/SLA, error budgets, percentiles, tail-at-scale amplification |
| 04 | [Networking Deep Dive](./04_networking_deep_dive.md) | TCP/IP, the three-way handshake, congestion control, TLS 1.3, DNS end to end, BGP and anycast, HTTP/1.1 vs 2 vs 3 |
| 05 | [Load Balancing, Proxies and Traffic Management](./05_load_balancing_proxies_traffic.md) | L4 vs L7, eight balancing algorithms, power-of-two-choices, health checks, VIP failover, load shedding |

### Part 2 — The Data Layer

| # | Chapter | What it covers |
| --- | --- | --- |
| 06 | [Storage Engine Internals](./06_storage_engines_internals.md) | How disks and SSDs really work, B+trees derived from scratch, LSM trees and compaction, write amplification, MVCC, row vs column layout |
| 07 | [Relational Databases and Transactions](./07_relational_databases_and_transactions.md) | The relational model, normalisation, reading query plans, index design, ACID traced through the write-ahead log, every isolation level and the anomaly it prevents |
| 08 | [NoSQL and Polyglot Persistence](./08_nosql_and_polyglot_persistence.md) | Key-value, document, wide-column, graph, time-series, search and vector stores; Redis, Cassandra, DynamoDB, MongoDB internals; a 25-case decision matrix |
| 09 | [Replication, Partitioning and Consistency](./09_replication_partitioning_consistency.md) | Leader-follower, multi-leader, leaderless quorums, sharding strategies, hotspots, resharding without downtime, CAP proved simply, PACELC in full |
| 10 | [Distributed Transactions and Data Integrity](./10_distributed_transactions_and_integrity.md) | Why two-phase commit blocks, sagas, the transactional outbox, idempotency keys, why "exactly once" is a lie, linearizability vs serializability |

### Part 3 — Moving and Accelerating Data

| # | Chapter | What it covers |
| --- | --- | --- |
| 11 | [Caching, CDNs and the Edge](./11_caching_cdn_and_edge.md) | Six caching strategies, seven eviction policies including W-TinyLFU, stampede/penetration/avalanche/hot-key with fixes, cache sizing from Zipf, CDN internals, video delivery |
| 12 | [Messaging and Event Streaming](./12_messaging_and_event_streaming.md) | Queue vs log, Kafka internals (segments, zero-copy, ISR, rebalancing, exactly-once), RabbitMQ/AMQP, SQS, Pulsar, NATS, backpressure, event sourcing and CQRS |
| 13 | [Big Data: Batch, Stream and Analytics](./13_big_data_batch_stream_analytics.md) | MapReduce from scratch, Spark, Flink, event time and watermarks, Lambda vs Kappa, star schemas, Parquet, the lakehouse |
| 14 | [Search Systems](./14_search_systems.md) | Building an inverted index by hand, BM25 derived, Lucene segments, Elasticsearch sharding, autocomplete, fuzzy matching, vector and hybrid search |

### Part 4 — Services and Infrastructure

| # | Chapter | What it covers |
| --- | --- | --- |
| 15 | [APIs and Communication Protocols](./15_apis_and_protocols.md) | REST done properly, pagination and idempotency, the protobuf wire format decoded by hand, gRPC's four streaming modes, GraphQL and the N+1 problem, WebSockets, webhooks |
| 16 | [Microservices and Service Architecture](./16_microservices_and_service_architecture.md) | When *not* to use microservices, domain-driven decomposition, the strangler fig, service discovery, service mesh, and nine resilience patterns with code |
| 17 | [Containers, Docker and Kubernetes](./17_containers_docker_kubernetes.md) | Linux namespaces and cgroups first, then Docker, then every Kubernetes object including StatefulSets, QoS classes, probes done right, the full packet path, and a debugging playbook |

### Part 5 — Running It in Production

| # | Chapter | What it covers |
| --- | --- | --- |
| 18 | [Security and Identity](./18_security_and_identity.md) | Threat modelling, password storage, JWT's real weaknesses, OAuth 2.0 and OIDC with sequence diagrams, RBAC/ABAC/ReBAC, secrets management, OWASP Top 10 with fixes, multi-tenancy isolation |
| 19 | [Observability and Operations](./19_observability_and_operations.md) | Logs, metrics, traces and profiles; PromQL; cardinality explosions; RED/USE/golden signals; SLO burn-rate alerts; incident response; blameless postmortems; chaos engineering |
| 20 | [Deployment, Multi-Region, Disaster Recovery and Cost](./20_deployment_multiregion_dr_cost.md) | CI/CD, blue-green vs canary, safe database migrations, feature flags, active-active vs active-passive, cell-based architecture, RPO/RTO, backup strategy, and the unit economics of cloud |

### Part 6 — Distributed Systems Theory

| # | Chapter | What it covers |
| --- | --- | --- |
| 21 | [Distributed Systems Theory and Consensus](./21_distributed_systems_theory_consensus.md) | The eight fallacies, failure models, physical/logical/vector/hybrid clocks, TrueTime, FLP impossibility, Paxos *and* Raft step by step, leases and fencing, gossip, the full CRDT catalogue |
| 22 | [Landmark Papers and Real Architectures](./22_landmark_papers_and_architectures.md) | GFS, MapReduce, Bigtable, Chubby, Dynamo, Spanner, Borg, Percolator, Zanzibar, Aurora, S3, TAO — each distilled into "what to steal, what aged badly" |

### Part 7 — Applying It

| # | Chapter | What it covers |
| --- | --- | --- |
| 23 | [Building Blocks and Algorithms](./23_building_blocks_and_algorithms.md) | Consistent hashing derived from failure, six rate limiters, Bloom/cuckoo/HyperLogLog/Count-Min with sizing formulas, geospatial indexing four ways, ID generation, deduplication, and machine learning in production |
| 24 | [Case Studies I — Foundations](./24_case_studies_part1.md) | URL shortener · Pastebin · Rate limiter · Key-value store · ID generator · Web crawler · Notification system · Typeahead |
| 25 | [Case Studies II — Advanced](./25_case_studies_part2.md) | News feed · Chat · Video streaming · File sync · Ride sharing · Ticketing · Payments · Maps · Ad click aggregation · Job scheduler · Object store · Backup & dedup · Metrics platform · Collaborative editing · **Config distribution control plane** |
| 26 | [The Interview Playbook](./26_interview_playbook_and_question_bank.md) | The 45-minute timeline, the six-step framework with scripts, level expectations, red flags, two annotated mock transcripts, and a 150+ question bank |

### Appendix

| # | Chapter | What it covers |
| --- | --- | --- |
| 99 | [Glossary and Cheat Sheets](./99_glossary_and_cheatsheets.md) | 400+ terms in one line each, plus every reference table in the book collected in one place |

## Diagrams

Every diagram is committed as a rendered **PNG** in [`diagrams/`](./diagrams/), so it is
visible in any Markdown viewer without needing Mermaid support. The Mermaid source is kept
beneath each image in a collapsible block so you can edit it.

After editing a diagram's source, re-render with:

```bash
python3 tools/render_diagrams.py
```

---

## Runnable code

Every algorithm in this book has a working Go implementation in [`code/`](./code/).
These are not snippets — they compile, they have tests, and each prints something
instructive when you run it. **No external dependencies**, so it all works offline.

```bash
cd code
go test ./...        # run every test — 23 packages
go run ./lru_cache   # run one example
```

| Directory | Implements | Chapter |
| --- | --- | --- |
| `lru_cache/` | LRU cache, O(1), map + doubly-linked list | 11 |
| `lfu_cache/` | LFU cache with frequency buckets | 11 |
| `consistent_hashing/` | Hash ring with virtual nodes | 23 |
| `bloom_filter/` | Bloom filter with optimal sizing | 23 |
| `count_min_sketch/` | Frequency estimation in sub-linear space | 23 |
| `hyperloglog/` | Cardinality estimation | 23 |
| `rate_limiter/` | Token bucket, leaky bucket, fixed and sliding window | 23 |
| `circuit_breaker/` | Three-state breaker with half-open probing | 16 |
| `retry_backoff_jitter/` | Exponential backoff with four jitter strategies | 16 |
| `bulkhead_semaphore/` | Concurrency isolation | 16 |
| `wal/` | Write-ahead log with CRC and crash recovery | 6 |
| `lsm_memtable/` | Skip-list memtable + SSTable flush | 6 |
| `b_tree/` | B+tree with splits and leaf chaining | 6 |
| `merkle_tree/` | Merkle tree for anti-entropy | 9 |
| `vector_clock/` | Vector clocks and conflict detection | 21 |
| `raft_election_sim/` | Raft leader election simulation | 21 |
| `snowflake_id/` | 64-bit distributed ID generation | 23 |
| `geohash/` | Geohash encode/decode and neighbours | 23 |
| `external_merge_sort/` | Sorting data far larger than memory | 13 |
| `worker_pool_pipeline/` | Goroutine pipeline with backpressure | 12 |
| `grpc_example/` | Protobuf wire format decoded by hand + RPC server/client | 15 |
| `outbox_pattern/` | Transactional outbox | 10 |
| `idempotency_keys/` | Idempotent request handling | 10 |

See [`code/README.md`](./code/README.md) for what each one is designed to prove.

Configuration examples live in [`config/`](./config/), annotated in the same style —
every non-obvious setting carries a comment explaining what breaks without it:

| File | Covers | Chapter |
| --- | --- | --- |
| `docker/Dockerfile` | Multi-stage build, distroless, non-root, exec-form entrypoint | 17 |
| `docker/docker-compose.yml` | Local stack with real health-check gating | 8, 12 |
| `docker/postgresql.conf` | Memory, WAL, autovacuum, `random_page_cost` on SSD | 6, 7 |
| `kubernetes/deployment.yaml` | The three probes, `preStop`, why no CPU limit | 17 |
| `kubernetes/service-hpa-pdb.yaml` | HPA behaviour, PDB, NetworkPolicy DNS trap | 17, 20 |
| `kubernetes/statefulset.yaml` | Stable identity, `volumeClaimTemplates`, quorum PDB | 17 |
| `nginx/nginx.conf` | Upstream keepalive, cache locking, stale-while-revalidate | 5, 11 |
| `prometheus/prometheus.yml` | Scrape config and cardinality control | 19 |
| `prometheus/alerts.yml` | Multi-window burn-rate alerting, symptoms over causes | 19 |
| `kafka/kafka.properties` | `acks=all`, `min.insync.replicas`, rebalance tuning | 12 |
| `code/grpc_example/order.proto` | Protobuf schema with field-numbering rules | 15 |

---

## Learning paths

Pick the one that matches your situation. See
[How to use this book](./00_how_to_use_this_book.md) for the detailed version.

### Path A — "I know nothing about backends"
`01 → 02 → 03 → 04 → 05 → 07 → 11 → 15 → 06 → 08 → 09 → 12 → 16 → 17 → 24`

Start at the very beginning and build vocabulary before theory. Twelve chapters in, you'll
be able to read an architecture diagram and know what every box does.

### Path B — "I have an interview in two weeks"
`02 → 03 → 09 (CAP/PACELC) → 11 → 12 → 23 → 24 → 25 → 26`

Optimised for the interview scoring rubric. Chapter 26 tells you exactly how you're graded;
read it first if you have less than a week.

### Path C — "I'm a working backend engineer levelling up"
`06 → 07 → 09 → 10 → 12 → 16 → 19 → 20 → 21 → 22`

Skips the fundamentals you already have. Focuses on the internals and the operational
reality that separates senior from mid-level.

### Path D — "I'm designing systems, not just building them"
`03 → 09 → 10 → 16 → 20 → 21 → 22 → 23 → 25`

Trade-off analysis, failure modes, multi-region topology, and the papers that everything
else is derived from.

---

## Notation used throughout

| Symbol | Meaning |
| --- | --- |
| **Bold term** | First appearance of a concept — it is being defined right here |
| `code font` | A literal command, identifier, header, or config key |
| ⚠️ | A trap that causes real production incidents |
| ⭐ | The load-bearing idea in this section — if you remember one thing, this |
| 💡 | A non-obvious insight worth remembering |
| 📐 | A formula or piece of arithmetic |

Units follow the storage convention used by engineers in practice:
1 KB = 1,000 bytes for capacity planning, and powers of two (KiB, MiB) only where the
hardware genuinely uses them. Where the distinction matters, it is called out explicitly.

---

## Sources and further reading

The ideas here are not original — the *explanations* are. If you want to go deeper than
this book takes you:

**Books**
- *Designing Data-Intensive Applications* — Martin Kleppmann. The single best book on this subject.
- *Database Internals* — Alex Petrov. Storage engines and distributed databases in depth.
- *Systems Performance* — Brendan Gregg. Performance analysis from the kernel up.
- *Site Reliability Engineering* — Google. Free online. The operational half of the picture.
- *Release It!* — Michael Nygard. Where the resilience patterns come from.

**Papers** (all distilled in [Chapter 22](./22_landmark_papers_and_architectures.md))
GFS · MapReduce · Bigtable · Chubby · Dynamo · Spanner · Borg · Percolator · Raft ·
Zanzibar · Aurora · Kafka · Cassandra · The Tail at Scale

**Primary documentation** — always prefer this over blog posts:
PostgreSQL, Kafka, Cassandra, Redis, Kubernetes, Envoy, gRPC, Elasticsearch, Prometheus.

---

## A note on how to read this

Do not read this book like a novel. Read a chapter, then close it and try to redraw the
mental-model diagram from memory. Then answer the "test yourself" questions. The material
only sticks if you reconstruct it rather than recognise it.

If a chapter feels too easy, you're on the wrong path — jump ahead. If it feels
impossible, back up to the chapters listed in its **Prerequisites** line. Nothing in this
book requires talent, only sequence.

**Start here → [Chapter 1: From Zero](./01_from_zero_computers_networks_web.md)**
