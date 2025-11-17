# RFC Prioritization Matrix

**Analysis Commit**: [`bd6aa865`](https://github.com/pingcap/tidb/tree/bd6aa865308ed409fd7010af83a84249bf398d8c)
**Date**: 2025-11-17

## Overview

This document prioritizes 10 proposed improvements to TiDB based on **impact** vs. **effort** analysis. Each RFC addresses specific pain points discovered during codebase analysis.

---

## Prioritization Criteria

### Impact (1-5)
- **5**: Critical performance/reliability improvement or significant new capability
- **4**: Major improvement in developer experience or operational efficiency
- **3**: Moderate improvement in specific use case
- **2**: Nice-to-have enhancement
- **1**: Minor improvement

### Effort (1-5 dev-weeks)
- **1**: <1 week (quick win)
- **2**: 1-2 weeks (small project)
- **3**: 2-4 weeks (medium project)
- **4**: 4-8 weeks (large project)
- **5**: >8 weeks (strategic initiative)

###

 Priority Score
```
Priority = Impact × (6 - Effort)
```
Higher score = Higher priority

---

## RFC Matrix

| RFC # | Title | Impact | Effort | Score | Category |
|-------|-------|--------|--------|-------|----------|
| **RFC-0001** | Plan Cache Warmup on Server Start | 4 | 2 | 16 | **Quick Win** |
| **RFC-0002** | Adaptive Memory Spill Thresholds | 5 | 3 | 15 | **Strategic** |
| **RFC-0003** | Query Hint Validation and Suggestions | 3 | 1 | 15 | **Quick Win** |
| **RFC-0004** | Background Statistics Refresh Optimization | 4 | 3 | 12 | **Strategic** |
| **RFC-0005** | Prepared Statement Cache Sharing | 3 | 3 | 9 | **Strategic** |
| **RFC-0006** | Error Message Improvement Framework | 3 | 2 | 12 | **Quick Win** |
| **RFC-0007** | Distributed Deadlock Detection | 5 | 5 | 5 | **Long-term** |
| **RFC-0008** | Index Selection Explainability | 3 | 2 | 12 | **Quick Win** |
| **RFC-0009** | Chunk Pool Memory Optimization | 4 | 2 | 16 | **Quick Win** |
| **RFC-0010** | Slow Query Log Sampling and Aggregation | 4 | 2 | 16 | **Quick Win** |

---

## Category Breakdown

### 🚀 Quick Wins (Score ≥ 12, Effort ≤ 2)
High value, low effort - implement first

1. **RFC-0001**: Plan Cache Warmup (Score: 16)
2. **RFC-0003**: Query Hint Validation (Score: 15)
3. **RFC-0006**: Error Message Improvements (Score: 12)
4. **RFC-0008**: Index Selection Explainability (Score: 12)
5. **RFC-0009**: Chunk Pool Optimization (Score: 16)
6. **RFC-0010**: Slow Query Log Sampling (Score: 16)

### 📊 Strategic (Score ≥ 9, Effort 3-4)
Significant value, moderate investment

1. **RFC-0002**: Adaptive Memory Spill (Score: 15)
2. **RFC-0004**: Statistics Refresh Optimization (Score: 12)
3. **RFC-0005**: Prepared Statement Sharing (Score: 9)

### 🔮 Long-term (Effort ≥ 5)
High value, requires sustained effort

1. **RFC-0007**: Distributed Deadlock Detection (Score: 5)

---

## Detailed Prioritization

### Tier 1: Immediate Action (Q1 2026)

#### RFC-0001: Plan Cache Warmup
**Rationale**: Cold start problem affects production deployments
- **Impact**: 4 (reduces startup latency by ~30s for busy systems)
- **Effort**: 2 (2-week implementation)
- **Risk**: Low (isolated feature)

#### RFC-0009: Chunk Pool Optimization
**Rationale**: Memory allocation hotspot in every query
- **Impact**: 4 (10-15% reduction in memory pressure)
- **Effort**: 2 (1-2 weeks)
- **Risk**: Low (existing pool mechanism)

#### RFC-0010: Slow Query Log Sampling
**Rationale**: High-QPS systems overwhelmed by slow log volume
- **Impact**: 4 (reduces log I/O by 80%+ while preserving insights)
- **Effort**: 2 (1-2 weeks)
- **Risk**: Low (rate limiting already exists)

#### RFC-0003: Query Hint Validation
**Rationale**: Typos in hints silently ignored, causing confusion
- **Impact**: 3 (improves developer experience)
- **Effort**: 1 (<1 week)
- **Risk**: Low (validation only)

---

### Tier 2: Next Quarter (Q2 2026)

#### RFC-0002: Adaptive Memory Spill
**Rationale**: Fixed thresholds cause OOM or underutilize memory
- **Impact**: 5 (prevents OOMs, improves throughput)
- **Effort**: 3 (3-4 weeks)
- **Risk**: Medium (affects critical path)

#### RFC-0004: Statistics Refresh Optimization
**Rationale**: Auto-analyze causes production latency spikes
- **Impact**: 4 (reduces P99 latency spikes)
- **Effort**: 3 (3-4 weeks)
- **Risk**: Medium (optimizer-critical)

#### RFC-0006: Error Message Framework
**Rationale**: Generic errors frustrate developers
- **Impact**: 3 (improves troubleshooting time)
- **Effort**: 2 (2 weeks)
- **Risk**: Low (non-functional enhancement)

#### RFC-0008: Index Selection Explainability
**Rationale**: Optimizer choices opaque to users
- **Impact**: 3 (helps query tuning)
- **Effort**: 2 (2 weeks)
- **Risk**: Low (EXPLAIN extension)

---

### Tier 3: Future Consideration

#### RFC-0005: Prepared Statement Sharing
**Rationale**: Connection pools waste memory on duplicate plans
- **Impact**: 3 (memory savings in specific scenarios)
- **Effort**: 3 (3 weeks, requires cache redesign)
- **Risk**: Medium (concurrency challenges)

#### RFC-0007: Distributed Deadlock Detection
**Rationale**: Deadlocks currently require manual intervention
- **Impact**: 5 (critical for high-contention workloads)
- **Effort**: 5 (8-12 weeks, requires distributed algorithm)
- **Risk**: High (complex, affects transaction layer)
- **Note**: Requires RFC process with broader community input

---

## Implementation Roadmap

### Phase 1: Quick Wins (Weeks 1-6)
```
Week 1-2:  RFC-0003 (Query Hint Validation)
Week 3-4:  RFC-0009 (Chunk Pool Optimization)
Week 5-6:  RFC-0010 (Slow Query Log Sampling)
```

### Phase 2: High-Impact Projects (Weeks 7-14)
```
Week 7-8:  RFC-0001 (Plan Cache Warmup)
Week 9-12: RFC-0002 (Adaptive Memory Spill)
Week 13-14: RFC-0006 (Error Message Framework)
```

### Phase 3: Strategic Improvements (Weeks 15-22)
```
Week 15-18: RFC-0004 (Statistics Optimization)
Week 19-20: RFC-0008 (Index Selection Explainability)
Week 21-22: RFC-0005 (Prepared Statement Sharing)
```

### Phase 4: Long-term (Future)
```
RFC-0007: Distributed Deadlock Detection (requires community RFC, 12+ weeks)
```

---

## Success Metrics

Each RFC defines specific success criteria. Aggregate metrics:

### Performance
- **Cold start latency**: -30% (RFC-0001)
- **Memory usage**: -15% (RFC-0009)
- **P99 latency**: -20% (RFC-0002, RFC-0004)

### Operational
- **Slow log volume**: -80% (RFC-0010)
- **Mean time to resolution (MTTR)**: -30% (RFC-0006, RFC-0008)

### Developer Experience
- **Query hint errors caught**: 100% (RFC-0003)
- **Optimizer choice understanding**: +50% (RFC-0008)

---

## Risk Mitigation

### High-Risk RFCs
- **RFC-0007** (Deadlock Detection): Requires extensive testing, gradual rollout

### Medium-Risk RFCs
- **RFC-0002** (Memory Spill): Requires feature flag, A/B testing
- **RFC-0004** (Statistics): Requires backoff mechanisms, monitoring

### Low-Risk RFCs
All others: Standard development process

---

## Stakeholder Approval Requirements

| RFC | Requires Approval From |
|-----|------------------------|
| RFC-0001, 0009, 0010 | Execution Team Lead |
| RFC-0002, 0004 | Execution Team + Planner Team |
| RFC-0003, 0006, 0008 | Any Senior Engineer |
| RFC-0005 | Session Team + Planner Team |
| RFC-0007 | Architecture Committee + Transaction Team |

---

## Next Steps

1. **Review RFCs**: Read individual RFC documents
2. **Prioritize**: Adjust based on team capacity and business priorities
3. **Assign Owners**: Each RFC needs a designated owner
4. **Create Issues**: File GitHub issues for approved RFCs
5. **Track Progress**: Use project board for RFC implementation status

---

## RFC Index

1. [RFC-0001: Plan Cache Warmup](./RFC-0001-plan-cache-warmup.md)
2. [RFC-0002: Adaptive Memory Spill](./RFC-0002-adaptive-memory-spill.md)
3. [RFC-0003: Query Hint Validation](./RFC-0003-query-hint-validation.md)
4. [RFC-0004: Statistics Refresh Optimization](./RFC-0004-statistics-refresh-optimization.md)
5. [RFC-0005: Prepared Statement Sharing](./RFC-0005-prepared-statement-sharing.md)
6. [RFC-0006: Error Message Framework](./RFC-0006-error-message-framework.md)
7. [RFC-0007: Distributed Deadlock Detection](./RFC-0007-distributed-deadlock-detection.md)
8. [RFC-0008: Index Selection Explainability](./RFC-0008-index-selection-explainability.md)
9. [RFC-0009: Chunk Pool Optimization](./RFC-0009-chunk-pool-optimization.md)
10. [RFC-0010: Slow Query Log Sampling](./RFC-0010-slow-query-log-sampling.md)

---

**Document Status**: Draft
**Last Updated**: 2025-11-17
**Owner**: Analysis Team
