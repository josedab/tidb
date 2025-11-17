# TiDB Deep Dive Blog Series

This blog series provides comprehensive technical analysis of TiDB's architecture and implementation.

## Published Posts

1. **[Architecture Overview](./01-architecture-overview.md)** ✅ Complete
   - Core concepts and design decisions
   - Component architecture (TiDB, TiKV, PD, TiFlash)
   - Trade-off analysis

## Remaining Posts (Summaries Provided)

The following posts cover advanced topics. Detailed versions can be developed based on the analysis data gathered:

2. **SQL Journey - From Client to Storage** - Query execution pipeline end-to-end
3. **Query Optimization Secrets** - Planner internals and cost-based optimization
4. **Distributed Transactions** - 2PC, MVCC, and conflict resolution  
5. **Execution Engine Internals** - Operators, chunks, and memory management
6. **Schema Changes at Scale** - Online DDL deep dive
7. **Production-Grade Observability** - Metrics, logging, and debugging

## Analysis Source

All analysis based on commit: [`bd6aa865`](https://github.com/pingcap/tidb/tree/bd6aa865308ed409fd7010af83a84249bf398d8c)

## Key Insights for Future Posts

### Post 2: Query Execution
- Entry point: `pkg/server/conn.go:handleQuery()`
- Pipeline: Parse → Plan → Optimize → Execute
- Chunk-based batch processing (1024 rows default)

### Post 3: Optimization
- 15+ optimization rules in sequence
- Join reordering: DP algorithm (<7 tables), Greedy (≥7 tables)
- Statistics: Histogram + CM-Sketch + TopN
- Plan cache: Per-session (prepared) + shared (non-prepared)

### Post 4: Transactions
- TSO from PD for global timestamps
- Percolator 2PC: Prewrite → Commit  
- Optimistic vs. Pessimistic modes
- Lock TTL and cleanup

### Post 5: Execution
- Iterator model: Open → Next → Close
- Hash join with spill-to-disk
- Memory tracking and quota enforcement
- Coprocessor push-down to TiKV

### Post 6: DDL
- State machine: None → Delete Only → Write Only → Public
- Owner election via etcd
- Schema lease (45s default)
- Reorg workers for index backfilling

### Post 7: Observability
- 60+ Prometheus metric families
- Slow query log: 60+ fields
- OpenTracing + Jaeger integration
- Flight recorder for post-mortem

## See Also

- [Initial Analysis](../initial-analysis/) - Comprehensive technical data
- [RFCs](../rfcs/) - Proposed improvements based on analysis
- [Diagrams](../diagrams/) - Architecture visualizations
