# TiDB Deep Dive: A Technical Blog Series

**Analysis Based on Commit**: [`bd6aa865`](https://github.com/pingcap/tidb/tree/bd6aa865308ed409fd7010af83a84249bf398d8c)

**Target Audience**: Developers familiar with databases and Go, interested in distributed systems and database internals

**Series Goal**: Provide a comprehensive technical understanding of TiDB's architecture, implementation patterns, and operational characteristics through hands-on exploration of the codebase.

---

## Series Overview

This 7-part series explores TiDB from first principles to advanced implementation details. Each post includes:
- Architectural diagrams
- Real code examples with commit SHA links
- Practical insights for contributors and operators
- Trade-off analyses

---

## Blog Posts

### **Post 1: Understanding TiDB - Architecture and Core Concepts**
**Length**: ~2,200 words
**Read Time**: 15 minutes

**What You'll Learn**:
- TiDB's position in the distributed database landscape
- High-level architecture: TiDB, TiKV, PD, and TiFlash
- Core abstractions and design decisions
- How TiDB achieves MySQL compatibility with distributed scalability

**Key Topics**:
- Separation of compute and storage
- Strong consistency via Percolator 2PC
- HTAP capabilities
- When to use TiDB vs. alternatives

**Target Reader**: New to TiDB or distributed databases

---

### **Post 2: The SQL Journey - From Client to Storage**
**Length**: ~2,500 words
**Read Time**: 17 minutes

**What You'll Learn**:
- Complete lifecycle of a SQL query
- Parsing, planning, and optimization pipeline
- Execution engine and iterator model
- How TiDB coordinates with TiKV

**Key Topics**:
- MySQL protocol handling in `pkg/server/`
- Parser internals and AST generation
- Logical vs. physical plans
- Push-down computation to coprocessors
- Chunk-based execution model

**Code Deep-Dives**:
- `cmd/tidb-server/main.go` - Server initialization
- `pkg/server/conn.go` - Query dispatch
- `pkg/planner/core/optimizer.go` - Optimization pipeline
- `pkg/executor/adapter.go` - Execution coordination

**Target Reader**: Developers wanting to understand query processing

---

### **Post 3: Query Optimization Secrets - Plans, Statistics, and Costing**
**Length**: ~2,400 words
**Read Time**: 16 minutes

**What You'll Learn**:
- How TiDB's query optimizer works
- Rule-based vs. cost-based optimization
- Statistics collection and usage
- Plan caching strategies

**Key Topics**:
- 15+ optimization rules explained
- Join reordering algorithms (DP vs. Greedy)
- Index selection logic
- Plan cache hit optimization
- Common optimization pitfalls

**Code Deep-Dives**:
- `pkg/planner/core/logical_plan_builder.go` - Plan construction
- `pkg/planner/core/optimizer.go` - Rule application
- `pkg/statistics/` - Statistics framework
- `pkg/planner/core/task.go` - Cost model

**Target Reader**: Query performance tuning enthusiasts

---

### **Post 4: Distributed Transactions - 2PC, MVCC, and Conflict Resolution**
**Length**: ~2,300 words
**Read Time**: 15 minutes

**What You'll Learn**:
- How distributed ACID transactions work in TiDB
- Percolator-based 2PC protocol
- MVCC implementation in TiKV
- Optimistic vs. pessimistic transactions

**Key Topics**:
- Timestamp Oracle (TSO) in PD
- Two-phase commit flow
- Write conflict detection and resolution
- Lock TTL and garbage collection
- Transaction retry strategies

**Code Deep-Dives**:
- `pkg/session/txn.go` - Transaction lifecycle
- `pkg/sessiontxn/` - Transaction context
- `pkg/kv/kv.go` - Transaction interface
- Integration with `tikv/client-go`

**Target Reader**: Developers interested in distributed systems

---

### **Post 5: Execution Engine Internals - Operators, Chunks, and Memory Management**
**Length**: ~2,600 words
**Read Time**: 18 minutes

**What You'll Learn**:
- How executors process data efficiently
- Volcano-style iteration with chunk batching
- Memory tracking and spill-to-disk
- Join and aggregation algorithms

**Key Topics**:
- Executor interface and lifecycle
- Chunk structure (columnar batching)
- Hash join implementation
- Stream vs. hash aggregation
- Memory quota enforcement
- Temporary storage management

**Code Deep-Dives**:
- `pkg/executor/builder.go` - Executor tree construction
- `pkg/executor/join.go` - Join implementations
- `pkg/executor/aggregate.go` - Aggregation executors
- `pkg/util/chunk/` - Chunk framework
- `pkg/util/memory/` - Memory tracking

**Target Reader**: Performance engineers, contributors

---

### **Post 6: Schema Changes at Scale - Online DDL Deep Dive**
**Length**: ~2,100 words
**Read Time**: 14 minutes

**What You'll Learn**:
- How TiDB performs non-blocking schema changes
- State transition protocol
- Owner election and coordination
- Challenges and limitations

**Key Topics**:
- Online DDL algorithm (based on Google F1)
- Schema versioning and leases
- DDL owner election via etcd
- Reorg workers for index backfilling
- Lightning for fast index creation

**Code Deep-Dives**:
- `pkg/ddl/ddl.go` - DDL executor
- `pkg/ddl/owner/` - Owner election
- `pkg/domain/domain.go` - Schema reload
- `pkg/meta/` - Metadata storage

**Target Reader**: Database administrators, platform engineers

---

### **Post 7: Production-Grade Observability - Metrics, Logging, and Debugging**
**Length**: ~2,000 words
**Read Time**: 13 minutes

**What You'll Learn**:
- TiDB's comprehensive observability infrastructure
- Metrics, logging, and tracing systems
- Debugging slow queries
- Profiling and performance analysis

**Key Topics**:
- Prometheus metrics (60+ families)
- Slow query log anatomy (60+ fields)
- OpenTracing integration with Jaeger
- Flight recorder for post-mortem analysis
- CPU/memory profiling
- Top SQL tracking

**Code Deep-Dives**:
- `pkg/metrics/` - Metric definitions
- `pkg/util/logutil/` - Logging framework
- `pkg/sessionctx/variable/slow_log.go` - Slow query formatting
- `pkg/util/traceevent/` - Trace events
- `pkg/domain/topn_slow_query.go` - Top-N tracking

**Target Reader**: SREs, production operators

---

## Reading Paths

### For New Contributors
**Recommended Order**: 1 → 2 → 5 → 3

Start with architecture, understand query flow, learn execution internals, then dive into optimization.

### For Performance Engineers
**Recommended Order**: 3 → 5 → 7 → 2

Focus on optimization, execution, observability, then fill in protocol details.

### For DBAs and Operators
**Recommended Order**: 1 → 7 → 6 → 4

Understand architecture, learn observability, master DDL, then dive into transactions.

### For Distributed Systems Enthusiasts
**Recommended Order**: 1 → 4 → 6 → 5

Architecture first, then transactions, DDL coordination, and execution.

---

## Code Navigation Tips

### Exploring on GitHub
All code references use commit SHA for stable links:

**Format**:
```
https://github.com/pingcap/tidb/blob/bd6aa865/<file_path>#L<line_number>
```

**Example**:
```
https://github.com/pingcap/tidb/blob/bd6aa865/cmd/tidb-server/main.go#L280
```

### Running Locally

```bash
# Clone the repository
git clone https://github.com/pingcap/tidb.git
cd tidb
git checkout bd6aa865

# Build
make

# Run tests for specific package
cd pkg/executor
go test -v -run TestHashJoin
```

---

## Series Goals

By the end of this series, you should be able to:

✅ Understand TiDB's layered architecture
✅ Trace a SQL query through the entire system
✅ Explain how distributed transactions work
✅ Optimize queries using statistics and plans
✅ Debug production issues using logs and metrics
✅ Contribute code to TiDB with confidence

---

## Complementary Resources

- **TiDB Design Documents**: [`docs/design/`](https://github.com/pingcap/tidb/tree/bd6aa865/docs/design)
- **TiDB Development Guide**: https://pingcap.github.io/tidb-dev-guide/
- **User Documentation**: https://docs.pingcap.com/tidb/stable
- **Community**: Discord, Slack, GitHub Discussions

---

## Writing Style

Each blog post follows a conversational yet technical style:

- **Code First**: Real examples from the codebase
- **Visuals**: Diagrams for complex flows
- **Why, Not Just What**: Explain trade-offs and design decisions
- **Practical**: Actionable insights for real-world usage
- **Accessible**: Assume familiarity with Go and databases, explain TiDB-specific concepts

---

## Get Started

**Begin with**: [Post 1 - Understanding TiDB: Architecture and Core Concepts](./01-architecture-overview.md)

---

**Feedback and Contributions**: These blog posts are based on code analysis at commit `bd6aa865`. If you notice inaccuracies or have suggestions, please file an issue or submit a PR.
