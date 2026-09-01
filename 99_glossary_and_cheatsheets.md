# Appendix — Glossary and Cheat Sheets

[← Chapter 26](./26_interview_playbook_and_question_bank.md) · [Contents](./README.md)

Two parts: a **glossary** of ~400 terms, each in one line, and a set of **cheat sheets**
collecting the reference tables from across the book into one place.

Use the glossary when a term appears and you want the shape of it in five seconds. Use the cheat
sheets the night before an interview.

---

# Part 1 — Glossary

## A

**ACID** — Atomicity, Consistency, Isolation, Durability: the transaction guarantees of a relational database ([Ch 7](./07_relational_databases_and_transactions.md)).
**Active-active** — Multiple regions serving writes simultaneously; requires conflict resolution.
**Active-passive** — One region serves; another stands by. Simpler, with failover downtime.
**Adaptive bitrate (ABR)** — Video player switches rendition per segment based on measured throughput.
**Aggregation pipeline** — A staged transformation of documents (MongoDB) or events (streams).
**AJAX** — Asynchronous JavaScript request; the origin of the single-page-app request pattern.
**Amdahl's Law** — Speedup is bounded by the serial fraction: $S = 1/(s + p/N)$ ([Ch 2](./02_scalability_and_estimation.md)).
**Anti-entropy** — Background process comparing replicas and repairing divergence; mandatory for convergence.
**API gateway** — Single entry point handling auth, rate limiting, routing and aggregation.
**Append-only log** — A structure written only at the end; the basis of WAL, Kafka and event sourcing.
**Argon2** — Memory-hard password hashing function; current recommended default.
**ARP** — Address Resolution Protocol; maps IP to MAC on a local network.
**At-least-once** — Delivery guarantee permitting duplicates; requires idempotent consumers.
**At-most-once** — Delivery guarantee permitting loss; no duplicates.
**Atomic** — Indivisible: all effects apply or none do.
**Autoscaling** — Adding/removing capacity based on a metric; ⚠️ reacts in minutes, not milliseconds.
**Availability** — Fraction of time a system serves requests successfully ([Ch 3](./03_reliability_availability_performance.md)).
**Availability Zone (AZ)** — An isolated datacenter within a cloud region.
**Avro** — Binary serialisation format with schema evolution; common in data pipelines.

## B

**Backpressure** — Signalling upstream to slow down rather than dropping or buffering unboundedly.
**Backfill** — Reprocessing historical data through a new pipeline.
**Base64** — Binary-to-text encoding; ⚠️ 33% size increase, not encryption.
**BASE** — Basically Available, Soft state, Eventual consistency; the informal counterpart to ACID.
**Bastion host** — A hardened jump server for administrative access to private networks.
**Batch processing** — Processing bounded datasets, typically on a schedule ([Ch 13](./13_big_data_batch_stream_analytics.md)).
**B-tree** — Balanced tree index; read-optimised, in-place updates ([Ch 6](./06_storage_engines_internals.md)).
**Bcrypt** — Adaptive password hashing function with a tunable cost factor.
**Bin packing** — Placing workloads onto machines to maximise utilisation; the scheduler's problem.
**Blast radius** — How much breaks when one thing fails; the thing deployment strategies minimise.
**Bloom filter** — Probabilistic set membership: no false negatives, tunable false positives ([Ch 23](./23_building_blocks_and_algorithms.md)).
**Blue-green deployment** — Two full environments; switch traffic instantly ([Ch 20](./20_deployment_multiregion_dr_cost.md)).
**Bulkhead** — Isolated resource pools so one failing dependency can't exhaust everything.
**Byzantine fault** — A node behaving arbitrarily or maliciously, not merely crashing.

## C

**Cache-aside** — Application checks cache, on miss reads the store and populates. The default pattern.
**Cache hit ratio** — Fraction of reads served from cache; ⭐ the number that determines database load.
**Cache stampede** — Many simultaneous misses on one hot key; fixed with single-flight + jitter.
**Canary deployment** — Route a small traffic percentage to a new version and watch metrics.
**CAP theorem** — Under a network *partition*, choose consistency or availability ([Ch 9](./09_replication_partitioning_consistency.md)).
**Cardinality** — Number of distinct values; ⚠️ high-cardinality labels kill time-series databases.
**Cassandra** — Wide-column AP store using consistent hashing and tunable quorums.
**CDC (Change Data Capture)** — Streaming a database's change log to other systems ([Ch 12](./12_messaging_and_event_streaming.md)).
**CDN** — Geographically distributed cache of content near users ([Ch 11](./11_caching_cdn_and_edge.md)).
**Cell-based architecture** — Independent, isolated stacks each serving a subset of users.
**Checkpoint** — Periodic durable snapshot of stream-processing state, enabling exactly-once state.
**Chaos engineering** — Deliberately injecting failures to validate resilience.
**Circuit breaker** — Stops calling a failing dependency after a threshold; prevents retry amplification.
**Client-side load balancing** — Client picks the backend; removes a network hop and a component.
**Clock skew** — Disagreement between machine clocks; ⚠️ makes timestamp ordering unsafe.
**Cloud-native** — Designed for elastic, failure-prone infrastructure rather than fixed servers.
**Cold start** — Latency incurred when a serverless function or cache begins empty.
**Column-oriented storage** — Values of one column stored contiguously; enables analytics compression.
**Compaction** — Merging SSTable levels in an LSM tree to reclaim space and bound reads.
**Compensating transaction** — An action that semantically undoes a completed saga step.
**Conflict-free Replicated Data Type (CRDT)** — Data type that converges without coordination ([Ch 21](./21_distributed_systems_theory_consensus.md)).
**Connection pool** — Reused database connections; ⚠️ its size is a common production bottleneck.
**Consensus** — Agreement on a single value among nodes despite failures (Raft, Paxos).
**Consistent hashing** — Hash ring minimising key movement when nodes change ([Ch 23](./23_building_blocks_and_algorithms.md)).
**Container** — Process isolation using namespaces and cgroups; not a virtual machine ([Ch 17](./17_containers_docker_kubernetes.md)).
**Content-addressed storage** — Objects keyed by hash of their content; enables deduplication.
**Contraction Hierarchies** — Preprocessing that makes shortest-path queries ~1000× faster.
**Control plane** — The system that configures the data plane; ⭐ the data plane should survive without it.
**CORS** — Browser mechanism controlling cross-origin requests.
**CQRS** — Separate models for reads and writes; often paired with event sourcing.
**Cross-AZ traffic** — Data transfer between availability zones; ⚠️ a frequently overlooked cost line.
**CRUD** — Create, Read, Update, Delete.
**CSRF** — Attack using a victim's ambient credentials; mitigated by tokens and SameSite cookies.
**Cursor pagination** — Paginating by a key rather than an offset; ⭐ O(1) instead of O(offset).

## D

**Data lake** — Raw data stored in open formats on object storage, schema applied at read time.
**Data plane** — The path that carries user traffic, as opposed to the control plane.
**Data warehouse** — Structured, modelled analytical store optimised for queries ([Ch 13](./13_big_data_batch_stream_analytics.md)).
**Dead letter queue (DLQ)** — Destination for messages that failed terminally; ⚠️ useless if unmonitored.
**Deduplication** — Storing identical content once, keyed by hash ([Ch 6](./06_storage_engines_internals.md)).
**Delta-of-delta encoding** — Compressing regular timestamps to ~1 bit each.
**Denormalisation** — Duplicating data to avoid joins; buys read speed, costs write complexity.
**Diffie-Hellman** — Key exchange allowing two parties to derive a shared secret over a public channel.
**Dijkstra's algorithm** — Single-source shortest path; too slow for interactive routing at scale.
**Distributed lock** — Mutual exclusion across processes; ⚠️ unsafe without fencing tokens.
**Distributed monolith** — Services deployed separately but coupled; all the cost, none of the benefit.
**DNS** — Maps names to addresses; ⚠️ TTLs make it a slow failover mechanism.
**Docker** — Tooling that popularised container images and the layered filesystem.
**Downsampling** — Storing coarser aggregates for older data to bound storage.
**Durability** — Committed data survives crashes; usually via WAL + replication.
**Dynamo** — Amazon's AP key-value store; the origin of consistent hashing + quorums in practice.

## E

**Edge computing** — Executing logic at CDN points of presence rather than origin.
**Elasticsearch** — Distributed search engine built on Lucene ([Ch 14](./14_search_systems.md)).
**Encryption at rest** — Protects against media theft; ⚠️ not against a compromised application.
**Encryption in transit** — TLS; protects against network observation and tampering.
**Erasure coding** — Splits data into k data + m parity shards; better durability than replication at lower cost.
**Error budget** — The allowed unreliability implied by an SLO; ⭐ turns reliability into arithmetic.
**etcd** — Raft-backed consistent key-value store; the Kubernetes datastore.
**Eventual consistency** — Replicas converge if writes stop; ⚠️ gives no bound on "eventually".
**Event sourcing** — Persisting state as an append-only sequence of events ([Ch 16](./16_microservices_and_service_architecture.md)).
**Event time** — The time an event occurred, as opposed to when it was processed ([Ch 13](./13_big_data_batch_stream_analytics.md)).
**Exactly-once** — ⚠️ Impossible for *delivery*; achievable for *state* within one system.
**Exponential backoff** — Increasing retry delays; ⚠️ requires jitter to avoid synchronised waves.
**Expand-migrate-contract** — Three-phase schema change enabling rollback at every step.

## F

**Failover** — Promoting a standby to primary after failure.
**Fail open / fail closed** — On dependency failure, allow or deny. ⭐ Defensive systems fail open; correctness systems fail closed.
**Fan-out** — Delivering one write to many destinations (feeds, notifications).
**Fault tolerance** — Continuing correct operation despite component failures.
**Fencing token** — Monotonic number attached to lock holders so stale holders are rejected ([Ch 21](./21_distributed_systems_theory_consensus.md)).
**FLP impossibility** — No deterministic consensus with one faulty process in a fully asynchronous model.
**Flink** — Stream processor with event-time semantics and exactly-once state.
**Follow-the-sun** — Routing traffic to the region nearest active users.
**Fsync** — Forces buffered writes to durable storage; the expensive part of durability.

## G

**GCRA** — Generic Cell Rate Algorithm; a rate limiter storing one timestamp per key ([Ch 23](./23_building_blocks_and_algorithms.md)).
**Geohash** — Encoding of a location into a string prefix; ⚠️ neighbours can differ in prefix.
**Gossip protocol** — Nodes randomly exchange state; converges in O(log N) rounds without a coordinator.
**Graceful degradation** — Serving a reduced but useful response instead of an error.
**GraphQL** — Query language letting clients specify exactly the fields they need ([Ch 15](./15_apis_and_protocols.md)).
**gRPC** — RPC framework over HTTP/2 with protobuf; supports streaming.
**Gorilla** — Facebook's time-series compression: ~1.4 bytes per data point.

## H

**H3** — Uber's hexagonal geospatial index; ⭐ all neighbours equidistant, unlike squares.
**Haystack** — Facebook's photo store; packs small objects into large containers.
**HDFS** — Distributed filesystem underlying the Hadoop ecosystem.
**Head-of-line blocking** — One slow item blocking those behind it; occurs at HTTP/1.1 and TCP layers.
**Health check** — Probe determining whether an instance should receive traffic; ⚠️ must check self, not dependencies.
**Heartbeat** — Periodic liveness signal; ⚠️ absence means "unreachable", not "dead".
**Hinted handoff** — Writing to a substitute node when the home replica is down; ⚠️ voids `W+R>N`.
**Horizontal scaling** — Adding machines rather than making one bigger.
**Hot partition** — A shard receiving disproportionate traffic; the usual sharding failure.
**HTTP/2** — Multiplexed streams over one connection; ⚠️ still TCP head-of-line blocked.
**HTTP/3** — HTTP over QUIC/UDP; independent streams eliminate transport-level blocking.
**Hybrid Logical Clock (HLC)** — Combines physical and logical time; captures causality with wall-clock meaning.

## I

**Idempotency** — Repeating an operation has the same effect as doing it once ([Ch 10](./10_distributed_transactions_and_integrity.md)).
**Idempotency key** — Client-supplied identifier deduplicating retried requests.
**Immutable infrastructure** — Replace rather than modify servers; eliminates configuration drift.
**Index** — Auxiliary structure making lookups fast; ⚠️ costs write throughput and space.
**Inverted index** — Maps terms to the documents containing them; the core of search ([Ch 14](./14_search_systems.md)).
**Isolation level** — How much concurrent transactions can observe each other ([Ch 7](./07_relational_databases_and_transactions.md)).

## J

**Jitter** — Randomness added to timers to prevent synchronisation; ⭐ essential in retries and TTLs.
**JWT** — Signed token carrying claims; ⚠️ revocation is hard because validation is stateless.
**Join** — Combining rows from multiple tables; the operation that resists sharding.

## K

**Kafka** — Distributed append-only log with partitioned, replayable topics ([Ch 12](./12_messaging_and_event_streaming.md)).
**Keyset pagination** — See *cursor pagination*.
**Kubernetes** — Container orchestrator built on reconciliation loops ([Ch 17](./17_containers_docker_kubernetes.md)).

## L

**Lambda architecture** — Parallel batch and streaming paths; ⚠️ justified only when their purposes differ.
**Lamport clock** — Logical counter giving a total order; ⚠️ cannot distinguish concurrency from causality.
**Latency** — Time for one operation; distinct from throughput.
**Leader election** — Choosing a single coordinator; requires consensus to be safe.
**Lease** — A lock with an expiry, so a dead holder doesn't block forever.
**Least connections** — Load balancing to the least busy backend; ⚠️ attracts traffic to fast-failing nodes.
**Linearizability** — Operations appear to take effect instantaneously in real-time order ([Ch 9](./09_replication_partitioning_consistency.md)).
**Little's Law** — $L = \lambda W$: concurrency = arrival rate × time in system.
**Load shedding** — Rejecting excess requests to protect the system; ⭐ acts in milliseconds.
**Locality-sensitive hashing (LSH)** — Hashing so similar items collide; enables near-duplicate detection.
**Log-structured merge tree (LSM)** — Write-optimised structure using memtables and SSTables.
**Long polling** — Client holds a request open until the server has data; a WebSocket alternative.
**Lucene** — The library underlying Elasticsearch and Solr.

## M

**MapReduce** — Batch model of map, shuffle and reduce over a cluster ([Ch 22](./22_landmark_papers_and_architectures.md)).
**Materialized view** — Precomputed query result maintained on write.
**Memtable** — In-memory sorted buffer in an LSM tree, flushed to an SSTable.
**Merkle tree** — Hash tree enabling efficient comparison of large datasets; used in anti-entropy.
**Message broker** — Middleware routing messages between producers and consumers.
**Microservices** — Independently deployable services; ⚠️ buys independence, costs a distributed system.
**Modular monolith** — One deployable with strict internal boundaries; ⭐ usually the right starting point.
**MTBF / MTTR** — Mean time between failures / to recovery; availability = MTBF/(MTBF+MTTR).
**Multi-tenancy** — One system serving multiple isolated customers.
**MVCC** — Multi-version concurrency control: readers see a snapshot, writers create new versions.

## N

**N+1 query** — One query per row of a result set; ⚠️ invisible in development, fatal in production.
**Negative caching** — Caching "not found" to prevent repeated misses reaching the database.
**Network partition** — Nodes alive but unable to communicate; the "P" in CAP.
**Nginx** — Widely-used reverse proxy and load balancer.
**Noisy neighbour** — One tenant degrading others by consuming shared resources.
**NoSQL** — Non-relational stores: document, wide-column, key-value, graph ([Ch 8](./08_nosql_and_polyglot_persistence.md)).

## O

**OAuth 2.0** — Delegated authorisation framework; ⚠️ authorisation, not authentication.
**Object storage** — Flat, HTTP-addressable, effectively unlimited storage (S3).
**Observability** — Ability to answer new questions about a system without shipping code ([Ch 19](./19_observability_and_operations.md)).
**OIDC** — Identity layer on top of OAuth 2.0; this is the authentication piece.
**OLAP / OLTP** — Analytical (few, large queries) vs transactional (many, small) workloads.
**Operational Transformation (OT)** — Collaborative editing via transformed operations; ⚠️ requires a central server.
**Optimistic concurrency** — Detect conflicts at commit rather than locking upfront.
**Outbox pattern** — Writing an event to the same transaction as the state change; ⭐ solves dual-write.

## P

**PACELC** — If Partitioned: A or C; Else: Latency or Consistency. ⭐ More useful than CAP.
**Paxos** — The original consensus algorithm; correct and notoriously hard to implement.
**Partition (data)** — A horizontal slice of data; synonym for shard.
**Partition key** — The value determining which shard holds a row; ⭐ the most consequential schema decision.
**Percentile (p50/p99/p999)** — Latency at a rank; ⚠️ averages hide the tail that users feel.
**Pessimistic locking** — Acquire locks before proceeding; safe, but reduces concurrency.
**Point of presence (PoP)** — A CDN edge location.
**Poison message** — A message that always fails; must go to a DLQ or it blocks the partition.
**Polyglot persistence** — Using different stores for different access patterns.
**Postmortem** — Blameless written analysis of an incident.
**Precomputation** — Doing work at write time to make reads O(1); the feed/typeahead pattern.
**Protobuf** — Compact binary serialisation with schema evolution.
**Publish/subscribe** — Producers publish to topics; subscribers receive independently.

## Q

**QPS** — Queries per second.
**Quorum** — A majority (or configured subset) required for an operation; `W+R>N` guarantees overlap.
**QUIC** — UDP-based transport underlying HTTP/3; eliminates transport head-of-line blocking.

## R

**Raft** — Consensus algorithm designed for understandability; strong leader, replicated log.
**RAID** — Disk redundancy schemes; ⚠️ not a backup.
**Rate limiting** — Bounding request rates; ⭐ a defensive control, so it should fail open.
**Read amplification** — Number of physical reads per logical read; the LSM cost.
**Read-modify-write** — A race-prone pattern; use atomic operations or CAS.
**Read repair** — Fixing stale replicas during a read; ⚠️ only fixes data that's read.
**Read replica** — A copy serving reads; ⚠️ doesn't add write capacity, and introduces lag.
**Read-your-writes** — A client always sees its own writes; ⭐ the most commonly needed session guarantee.
**Reconciliation loop** — Continuously drive actual state towards desired state; ⭐ level-triggered, so lost events self-correct.
**Redis** — In-memory data structure store; the default cache.
**Reed-Solomon** — The erasure code used by object stores.
**Region** — A geographic cloud location containing multiple AZs.
**Replication lag** — Delay between a primary write and its appearance on a replica.
**Retry storm** — Retries amplifying load on a struggling system; ⚠️ multiplies per layer.
**Reverse proxy** — Server-side intermediary handling TLS, routing, caching.
**RPO / RTO** — Recovery point (data loss) / recovery time objectives ([Ch 20](./20_deployment_multiregion_dr_cost.md)).
**RPC** — Remote procedure call.

## S

**Saga** — Long-running transaction as a sequence of local transactions with compensations.
**Scatter-gather** — Query all shards and merge; ⚠️ latency becomes the slowest shard's.
**Schema-on-read / on-write** — Structure applied at query time vs ingestion time.
**Serializability** — Transactions produce a result equivalent to some serial order.
**Server-sent events (SSE)** — One-way server→client streaming over HTTP.
**Service discovery** — Locating healthy service instances dynamically.
**Service mesh** — Sidecar proxies handling inter-service traffic concerns.
**Sharding** — Splitting data across nodes by key ([Ch 9](./09_replication_partitioning_consistency.md)).
**Sidecar** — A helper container running alongside an application container.
**SimHash** — Fingerprint where similar documents produce similar hashes.
**Single point of failure (SPOF)** — A component whose failure takes down the system.
**SLA / SLO / SLI** — Contract / internal target / measurement ([Ch 19](./19_observability_and_operations.md)).
**Sloppy quorum** — Accepting writes on non-home nodes for availability; ⚠️ voids the overlap guarantee.
**Snapshot isolation** — Transactions read a consistent snapshot; ⚠️ permits write skew.
**Snowflake ID** — 64-bit time-sortable ID: timestamp + machine + sequence.
**SPOF** — See *single point of failure*.
**Spanner** — Google's globally-distributed, externally-consistent database using TrueTime.
**Split brain** — Two nodes both believing they're primary; prevented by quorums and fencing.
**SQL injection** — Attacker-controlled data interpreted as SQL; prevented by parameterised queries.
**SSTable** — Immutable sorted file produced by flushing a memtable.
**Sticky session** — Pinning a client to one backend; ⚠️ makes scaling and deploys harder.
**Strangler fig** — Incrementally replacing a legacy system behind a facade.
**Stream processing** — Continuous processing of unbounded data ([Ch 13](./13_big_data_batch_stream_analytics.md)).
**Static stability** — ⭐ The data plane keeps working when the control plane is unavailable.
**Synthetic full** — A full backup assembled internally from incrementals; fixes restore fragmentation.

## T

**Tail latency** — The slow end of the distribution; ⚠️ amplified by fan-out.
**TCP** — Reliable, ordered byte stream; three-way handshake, congestion control.
**Thundering herd** — Many clients acting simultaneously after an event; fixed with jitter.
**Throughput** — Operations per unit time.
**Time-series database (TSDB)** — Store optimised for timestamped metrics; ⭐ ~48× compression vs naive.
**TLS** — Transport-layer encryption and authentication.
**Token bucket** — Rate limiter allowing bursts up to a bucket size.
**Tombstone** — A deletion marker in an LSM or replicated store; ⚠️ needed to prevent resurrection.
**Trace** — Causally-linked spans across services for one request.
**Transaction** — A unit of work with atomicity and isolation guarantees.
**TrueTime** — Google's bounded-uncertainty clock API; enables external consistency by waiting out the bound.
**Two-phase commit (2PC)** — Atomic commit across participants; ⚠️ blocks if the coordinator fails.
**Two Generals Problem** — Proves agreement is impossible over a lossy channel; hence no exactly-once delivery.

## U

**UDP** — Connectionless, unreliable datagram protocol; the basis of QUIC.
**Universal Scalability Law (USL)** — Throughput model with contention and coherency terms; ⭐ explains negative returns.
**UUID** — 128-bit identifier; ⚠️ v4 is a poor clustered primary key, v7 is time-sortable.

## V

**Vacuum** — Postgres process reclaiming dead tuples; ⚠️ blocked by long-running transactions.
**Vector clock** — Per-node counters detecting concurrent updates; O(N) size.
**Version vector** — Vector clock applied to replicas of a data item.
**Vertical scaling** — Making one machine bigger; simpler, and often correct.
**Virtual node (vnode)** — Multiple ring positions per physical node; smooths consistent-hashing distribution.
**Virtual waiting room** — Queue admitting users in batches; ⭐ converts a contention spike into ordinary load.

## W

**WAL (write-ahead log)** — Durability mechanism: log the change before applying it.
**Watermark** — Stream-processing estimate of event-time progress; decides when a window closes.
**WebSocket** — Full-duplex persistent connection over one TCP connection.
**Wide-column store** — Rows with dynamic columns, partitioned by key (Cassandra, HBase).
**Write amplification** — Physical writes per logical write; the dominant LSM and SSD cost.
**Write-behind** — Cache acknowledges, then writes to the store asynchronously; ⚠️ can lose data.
**Write skew** — Two transactions each valid alone jointly violating an invariant.
**Write-through** — Write to cache and store synchronously; consistent but slower.

## X–Z

**xDS** — Envoy's configuration distribution protocol; the reference config control plane.
**XSS** — Injecting scripts into pages viewed by others; mitigated by output encoding and CSP.
**Zero trust** — No implicit trust from network location; authenticate and authorise every request.
**Zipf distribution** — Popularity distribution where a small fraction of items gets most traffic; ⭐ why caching works.
**ZooKeeper** — Consensus-backed coordination service; used for leader election and config.

---

# Part 2 — Cheat sheets

## Sheet 1 — Latency numbers

```
L1 cache reference                        0.5 ns
Branch mispredict                           5 ns
L2 cache reference                          7 ns
Mutex lock/unlock                          25 ns
Main memory reference                     100 ns
Compress 1 KB (Snappy)                  2,000 ns   =   2 µs
Send 1 KB over 1 Gbps network          10,000 ns   =  10 µs
SSD random read                        16,000 ns   =  16 µs
Read 1 MB sequentially from memory     50,000 ns   =  50 µs
Round trip within same datacenter     500,000 ns   = 500 µs
Read 1 MB sequentially from SSD     1,000,000 ns   =   1 ms
Disk seek (HDD)                    10,000,000 ns   =  10 ms
Read 1 MB sequentially from HDD    20,000,000 ns   =  20 ms
Round trip US East → US West       60,000,000 ns   =  60 ms
Round trip US → Europe             80,000,000 ns   =  80 ms
Round trip US → Asia              150,000,000 ns   = 150 ms
```
⭐ **The three ratios worth internalising:** memory is ~100× faster than SSD; SSD is ~1000× faster
than an HDD seek; a same-DC round trip is ~5000× a memory access. And **cross-region latency is
physics** — you can't optimise it, only design around it.

## Sheet 2 — Throughput ceilings (single node, order of magnitude)

| Component | Typical ceiling |
| --- | --- |
| Redis | 100,000 ops/s |
| Memcached | 200,000+ ops/s |
| PostgreSQL reads (cached) | 50,000/s |
| PostgreSQL write TPS | 5,000–10,000 |
| MySQL write TPS | 5,000–15,000 |
| Cassandra writes/node | 10,000–50,000/s |
| Kafka broker | 100,000+ msg/s |
| Elasticsearch queries | 1,000–10,000/s |
| Nginx proxying | 50,000 req/s |
| Application server (Java/Go) | 5,000–20,000 req/s |
| WebSocket connections/server | 500,000 (tuned) |
| NVMe SSD IOPS | 100,000–1,000,000 |
| 10 GbE NIC | ~1.25 GB/s |

## Sheet 3 — Estimation conversions

```
TIME
1 day ≈ 100,000 seconds (86,400, rounded)
1 month ≈ 2.6M s · 1 year ≈ 31.5M s

RATES
1 million/day     ≈      12/s
10 million/day    ≈     120/s
100 million/day   ≈   1,200/s
1 billion/day     ≈  12,000/s
Peak ≈ 2-3× average (state your multiplier)

SIZES
1 KB × 1 million = 1 GB
1 MB × 1 million = 1 TB
1 GB × 1 million = 1 PB

POWERS OF TWO
2^10 ≈ 1 thousand   2^20 ≈ 1 million   2^30 ≈ 1 billion
2^32 ≈ 4.3 billion  2^40 ≈ 1 trillion  2^64 ≈ 1.8 × 10^19

TYPICAL SIZES
UUID 16 B · timestamp 8 B · int64 8 B
URL ~100 B · tweet ~300 B · web page ~2 MB
photo ~2 MB · 1 min 1080p video ~40 MB
```

## Sheet 4 — Availability

| Availability | Downtime/year | Downtime/month | Notes |
| --- | --- | --- | --- |
| 99% | 3.65 days | 7.2 hours | |
| 99.9% | 8.77 hours | 43.8 min | Common SaaS baseline |
| 99.95% | 4.38 hours | 21.9 min | |
| 99.99% | 52.6 min | 4.4 min | Requires multi-AZ + automation |
| 99.999% | 5.26 min | 26 s | ⚠️ Less than most deploy windows |

```
Serial dependencies:   A_total = A₁ × A₂ × ... × Aₙ    ⚠️ always worse
Parallel redundancy:   A_total = 1 − (1−A)^n            ✅ better
Availability from MTBF: A = MTBF / (MTBF + MTTR)
⭐ Reducing MTTR is usually cheaper than increasing MTBF.
```

## Sheet 5 — Cost (rough public-cloud, 2024–25)

```
Block storage (gp3)          $ 80 /TB/month
Object storage (S3 std)      $ 23 /TB/month
S3 Infrequent Access         $ 12 /TB/month
Glacier Deep Archive         $  1 /TB/month
⭐ Internet egress          $ 50-90 /TB      ← the usual surprise
Cross-AZ transfer            $ 20 /TB each direction
Cross-region transfer        $ 20 /TB
NAT Gateway processing       $ 45 /TB        ⚠️ often larger than the compute
Compute (general purpose)    ~$ 30 /vCPU/month on-demand
Spot instances               60-90% discount, interruptible
Reserved / savings plans     ~40-60% discount for 1-3 year commitment
```
⭐ **The optimisation order that actually works:** (1) delete unused resources, (2) rightsize,
(3) commit to reservations, (4) architect for spot, (5) reduce egress. ⚠️ Most teams start at (5)
and skip (1).

## Sheet 6 — Choosing a datastore

| Need | Choose | ⚠️ Watch out for |
| --- | --- | --- |
| Transactions, joins, flexible queries | PostgreSQL / MySQL | Write ceiling ~10k TPS |
| Very high write throughput, known access pattern | Cassandra / ScyllaDB | No joins; partition design is permanent |
| Sub-ms reads, ephemeral data | Redis | Memory cost; persistence is optional |
| Documents with varying shape | MongoDB / DocumentDB | Easy to skip schema design entirely |
| Analytics over billions of rows | ClickHouse / BigQuery / Snowflake | Not for OLTP |
| Full-text / faceted search | Elasticsearch / OpenSearch | Not a system of record |
| Time-series metrics | Prometheus / InfluxDB / Timescale | Cardinality explosions |
| Blobs, media, backups | S3 / GCS | Per-request costs at high object counts |
| Graph traversal | Neo4j / Neptune | Sharding graphs is genuinely hard |
| Strong consistency, global | Spanner / CockroachDB | Cost, and commit latency |

## Sheet 7 — Isolation levels

| Level | Dirty read | Non-repeatable read | Phantom | Write skew |
| --- | --- | --- | --- | --- |
| Read uncommitted | ✅ possible | ✅ | ✅ | ✅ |
| Read committed | ❌ | ✅ | ✅ | ✅ |
| Repeatable read | ❌ | ❌ | ✅* | ✅ |
| Snapshot isolation | ❌ | ❌ | ❌ | ⚠️ **possible** |
| Serializable | ❌ | ❌ | ❌ | ❌ |

\* Postgres's "repeatable read" is snapshot isolation and prevents phantoms.
⚠️ **Snapshot isolation permitting write skew is the trap** — two transactions each read a valid
state and each write, jointly breaking an invariant.

## Sheet 8 — Consistency models, strongest to weakest

```
Linearizable          Single-object recency; appears instantaneous
Sequential            All nodes see the same order; not necessarily real-time
Causal                Causally-related ops ordered; ⭐ strongest achievable
                      without coordination
Read-your-writes      You see your own writes
Monotonic reads       You never see time go backwards
Monotonic writes      Your writes apply in order
Eventual              Converges if writes stop; ⚠️ no bound
```

## Sheet 9 — Delivery guarantees

| Guarantee | Duplicates | Loss | Use when |
| --- | --- | --- | --- |
| At-most-once | ❌ | ✅ possible | Metrics, telemetry |
| **At-least-once** | ✅ possible | ❌ | ⭐ Default — pair with idempotency |
| Exactly-once (delivery) | — | — | ⚠️ Impossible (Two Generals) |
| Exactly-once (state) | — | — | Within one system, via checkpoints + transactional sink |

## Sheet 10 — Resilience patterns

| Pattern | Prevents | ⚠️ Cost |
| --- | --- | --- |
| Timeout | Unbounded waits | Too short → false failures |
| Retry + jittered backoff | Transient failures | Amplifies load; needs idempotency |
| Circuit breaker | Retry amplification | Tuning half-open behaviour |
| Bulkhead | Resource exhaustion spreading | Lower total utilisation |
| Load shedding | Total collapse | Rejecting real users |
| Rate limiting | Abuse and overload | Needs coordination decisions |
| Graceful degradation | All-or-nothing failure | More code paths to test |
| Hedged requests | Tail latency | ~5% extra load |
| Static stability | Control-plane dependence | Stale data during outages |

## Sheet 11 — Deployment strategies

| Strategy | Rollback speed | Cost | Mixed versions |
| --- | --- | --- | --- |
| Recreate | Slow | Low | No (downtime) |
| Rolling | Medium | Low | ⚠️ Yes |
| Blue-green | ⭐ Instant | 2× | No |
| Canary | Fast | ~1.1× | ⚠️ Yes |
| Feature flag | ⭐ Instant, per-user | Low | Yes (by design) |

⚠️ **Mixed versions mean forward and backward compatibility are mandatory**, in both APIs and
database schemas. That's why expand-migrate-contract exists.

## Sheet 12 — HTTP status codes worth using correctly

```
200 OK              201 Created (+ Location)   202 Accepted (async)
204 No Content      206 Partial Content
301 Moved Permanently ⚠️ cached forever      302 Found      304 Not Modified
400 Bad Request     401 Unauthenticated ⚠️ (misnamed)   403 Forbidden
404 Not Found       409 Conflict     412 Precondition Failed
422 Unprocessable   428 Precondition Required
429 Too Many Requests ⭐ always with Retry-After
500 Internal Error  502 Bad Gateway  503 Service Unavailable ⭐ + Retry-After
504 Gateway Timeout
```
⚠️ **401 means "not authenticated"; 403 means "authenticated but not permitted".** And a rate
limit is **429**, not 503 — 503 says *you* are broken.

## Sheet 13 — The decision checklist

Run through this on any design:

```
□ What's the read:write ratio?
□ What's the peak QPS, and what multiplier did I assume?
□ Does the working set fit in memory? At what hit rate?
□ ⭐ What's the partition key, and does the highest-volume query have it?
□ What happens when the cache is empty?
□ What happens during a network partition — do I choose C or A?
□ Which operations must be idempotent?
□ Where is the single point of failure?
□ ⭐ What's the blast radius of the worst deploy I could make?
□ How do I roll back? Has the schema change been split?
□ What do I alert on — symptoms or causes?
□ What's the biggest cost line, and is it egress?
□ What's my actual, measured RTO — not the aspirational one?
□ What did I give up, and was it worth it?
```

---

## Closing

⭐ **The one-sentence summary of this book:** every mechanism buys something and costs something,
and system design is the practice of knowing which trade you're making and why it's the right one
*here*.

Everything else — the algorithms, the protocols, the products — is vocabulary for expressing that.

---

[← Chapter 26](./26_interview_playbook_and_question_bank.md) · [Contents](./README.md)
