# TiDB Repository Structure

**Analysis Commit**: `bd6aa865308ed409fd7010af83a84249bf398d8c`

## Directory Tree Overview

```
tidb/
├── cmd/                    # Executable entry points
│   ├── tidb-server/       # Main TiDB server binary
│   ├── importer/          # Data import tool
│   └── explaintest/       # Explain plan testing tool
│
├── pkg/                    # Core TiDB packages (main codebase)
│   ├── bindinfo/          # SQL binding (query hints persistence)
│   ├── config/            # Configuration management
│   ├── ddl/               # Data Definition Language execution
│   ├── distsql/           # Distributed SQL coordination
│   ├── domain/            # Global metadata and lifecycle management
│   ├── errno/             # MySQL error codes and messages
│   ├── executor/          # Query execution engine
│   ├── expression/        # Expression evaluation and built-in functions
│   ├── infoschema/        # Metadata schema management
│   ├── kv/                # Key-value storage abstraction
│   ├── lock/              # Table lock implementation
│   ├── meta/              # Metadata storage in KV
│   ├── metrics/           # Prometheus metrics definitions
│   ├── owner/             # Distributed owner election
│   ├── parser/            # SQL parser (MySQL-compatible)
│   ├── planner/           # Query planning and optimization
│   ├── plugin/            # Plugin framework
│   ├── privilege/         # Access control and permissions
│   ├── server/            # MySQL protocol server
│   ├── session/           # Session and transaction management
│   ├── sessionctx/        # Session context interfaces
│   ├── statistics/        # Table statistics for optimization
│   ├── store/             # Storage driver implementation
│   ├── structure/         # Structured data types on KV
│   ├── table/             # Table abstraction
│   ├── tablecodec/        # SQL data ↔ KV encoding
│   ├── telemetry/         # Usage telemetry
│   ├── types/             # SQL type system
│   ├── ttl/               # Time-to-Live cleanup
│   └── util/              # Shared utilities
│
├── tests/                  # Test suites
│   ├── integrationtest/   # Integration tests (SQL-based)
│   ├── realtikvtest/      # Tests with real TiKV
│   ├── graceshutdown/     # Graceful shutdown tests
│   └── globalkilltest/    # Distributed kill tests
│
├── br/                     # Backup & Restore tool
├── dumpling/              # Data export tool (MySQL → files)
├── lightning/             # Data import tool (files → TiDB)
│
├── docs/                   # Documentation and design docs
├── build/                  # Build scripts and configurations
├── tools/                  # Development tools
│
├── Makefile               # Build targets
├── go.mod                 # Go module dependencies
├── BUILD.bazel            # Bazel build configuration
└── README.md              # Project overview
```

---

## Core Packages Deep Dive

### 1. Server Layer (`pkg/server/`)

**Purpose**: MySQL protocol implementation and connection management

**Key Files**:
- `server.go` (~1500 lines) - Main server struct, listener management
- `conn.go` (~3000 lines) - Client connection handler, protocol implementation
- `driver.go` - Storage driver interface
- `util.go` - Protocol utilities

**Responsibilities**:
- TCP/Unix socket listeners
- MySQL handshake and authentication
- Query command dispatching
- Result set serialization
- Connection pooling and limits (token-based)

**Dependencies**: `pkg/session`, `pkg/executor`, `pkg/parser`

---

### 2. Session Management (`pkg/session/`)

**Purpose**: Execution context for SQL statements

**Key Files**:
- `session.go` (~4000 lines) - Main session implementation
- `bootstrap.go` (~2000 lines) - Database bootstrap logic
- `txn.go` - Transaction lifecycle management

**Responsibilities**:
- Transaction coordination (begin/commit/rollback)
- Variable management (session and global)
- Prepared statement cache
- Statement execution history
- Resource tracking (memory, temp storage)

**Key Interfaces**:
```go
type Session interface {
    Execute(context.Context, string) (sqlexec.RecordSet, error)
    ExecuteStmt(context.Context, ast.StmtNode) (sqlexec.RecordSet, error)
    GetSessionVars() *variable.SessionVars
    Txn(bool) (kv.Transaction, error)
}
```

---

### 3. Parser (`pkg/parser/`)

**Purpose**: SQL syntax parsing and AST generation

**Key Components**:
- `parser.go` - Main parser implementation
- `lexer.go` - Lexical analyzer
- `ast/` - Abstract Syntax Tree node definitions
- `model/` - Schema metadata models
- `types/` - Type definitions

**Supported SQL**:
- MySQL 8.0 syntax
- TiDB-specific extensions (e.g., `ADMIN` commands)

**Output**: AST (Abstract Syntax Tree) for planner consumption

**Parser Generation**: Uses `goyacc` (yacc-based parser generator)

---

### 4. Planner (`pkg/planner/`)

**Purpose**: Transform AST into executable plans with optimizations

**Subdirectories**:

#### `planner/core/` - Core planning logic
**Files**:
- `optimizer.go` (~500 lines) - Main optimization pipeline
- `logical_plan_builder.go` (~5000+ lines) - AST → Logical plan
- `planbuilder.go` - Plan construction utilities
- `exhaust_physical_plans.go` - Physical plan enumeration
- `task.go` - Cost model for plan selection

**Optimization Rules** (applied in sequence):
1. Column Pruning - Remove unused columns
2. Build Key Info - Determine uniqueness constraints
3. Predicate Push Down - Move filters closer to data
4. Aggregation Elimination - Remove redundant aggregations
5. Projection Elimination - Remove unnecessary projections
6. Max/Min Elimination - Convert to index access
7. Outer Join Elimination - Convert to inner join where possible
8. Partition Pruning - Skip irrelevant partitions
9. Aggregation Push Down - Push aggregation to storage
10. Top-N Push Down - Limit at source
11. Join Reordering - Find optimal join order (DP algorithm)
12. Index Selection - Choose best index
13. Cost-based optimization - Final plan selection

#### `planner/core/operator/` - Operator definitions
- `logicalop/` - Logical operators (Selection, Projection, Join, etc.)
- `physicalop/` - Physical operators (HashJoin, MergeJoin, IndexScan, etc.)

**Key Abstractions**:
```go
type LogicalPlan interface {
    Schema() *expression.Schema
    Children() []LogicalPlan
    SetChildren(...LogicalPlan)
    // Optimization methods
}

type PhysicalPlan interface {
    LogicalPlan
    Cost() float64
    GetCost() *CostInfo
}
```

---

### 5. Executor (`pkg/executor/`)

**Purpose**: Execute physical plans and return results

**Execution Model**: Iterator-based (Volcano-style) with chunk batching

**Key Executors**:

#### Scan Executors
- `TableReaderExecutor` - Full table scan via TiKV
- `IndexReaderExecutor` - Index-only scan
- `IndexLookUpExecutor` - Index scan + table lookup
- `PointGetExecutor` - Single-row retrieval

#### Join Executors
- `HashJoinExec` - In-memory hash join
- `MergeJoinExec` - Sort-merge join
- `IndexJoinExec` - Nested-loop join with index

#### Aggregation Executors
- `HashAggExec` - Hash-based grouping
- `StreamAggExec` - Streaming aggregation (requires sorted input)

#### Modification Executors
- `InsertExec` - Row insertion
- `UpdateExec` - Row updates
- `DeleteExec` - Row deletion
- `ReplaceExec` - Replace (delete + insert)

#### Utility Executors
- `LimitExec` - Row limiting
- `SortExec` - Sorting (with spill-to-disk)
- `ProjectionExec` - Column projection
- `SelectionExec` - Row filtering

**Key Files**:
- `adapter.go` (~1500 lines) - Execution coordinator, slow query logging
- `builder.go` (~4000 lines) - Physical plan → Executor tree
- `executor.go` - Base executor interface
- `join.go`, `aggregate.go`, `sort.go` - Core algorithms

**Interface**:
```go
type Executor interface {
    Open(context.Context) error
    Next(context.Context, *chunk.Chunk) error
    Close() error
    Schema() *expression.Schema
}
```

**Chunk System**:
- Batch size: ~1024 rows (configurable)
- Column-oriented storage within chunk
- Memory-efficient iteration

---

### 6. DistSQL (`pkg/distsql/`)

**Purpose**: Coordinate distributed query execution on TiKV/TiFlash

**Key Files**:
- `distsql.go` - Request builder and coordinator
- `select_result.go` - Result streaming
- `stream.go` - Streaming result handler

**Features**:
- Coprocessor request generation
- Push-down filters, aggregations, projections
- MPP (TiFlash) coordination
- Region splitting awareness
- Retry and failover logic

**Request Types**:
- `Cop` - Coprocessor (TiKV row-based)
- `BatchCop` - Batch coprocessor (TiFlash)
- `MPP` - Massively Parallel Processing (TiFlash analytical queries)

---

### 7. Storage Layer (`pkg/store/`)

**Purpose**: Abstract storage backend

**Drivers**:
- `tikv` - Production TiKV driver
- `mockstore/unistore` - In-memory TiKV simulation
- `mockstore/mockstorage` - Test mock

**Key Files**:
- `store.go` - Driver registration and initialization
- `driver.go` - Storage driver interface

**Integration**: Via `pkg/kv` interfaces

---

### 8. KV Interface (`pkg/kv/`)

**Purpose**: Key-value storage abstraction

**Core Interfaces**:
```go
type Storage interface {
    Begin() (Transaction, error)
    GetSnapshot(version uint64) Snapshot
    Close() error
}

type Transaction interface {
    Get(ctx context.Context, k Key) ([]byte, error)
    Set(k Key, v []byte) error
    Delete(k Key) error
    Commit(ctx context.Context) error
    Rollback() error
    LockKeys(ctx context.Context, keys ...Key) error
}

type Snapshot interface {
    Get(ctx context.Context, k Key) ([]byte, error)
    Iter(k Key, upperBound Key) (Iterator, error)
}
```

**Key Files**:
- `kv.go` (~1000 lines) - Core interfaces
- `error.go` - Error types and retry logic
- `key.go` - Key encoding utilities
- `mem_buffer.go` - Write buffer implementation

---

### 9. DDL (`pkg/ddl/`)

**Purpose**: Schema change execution

**Key Features**:
- **Online DDL**: Schema changes without blocking reads/writes
- **Distributed coordination**: Single owner per cluster
- **Schema lease**: Ensures cross-node consistency

**Files**:
- `ddl.go` (~2000 lines) - Main DDL executor
- `table.go` - Table creation/modification
- `column.go` - Column operations (add/drop/modify)
- `index.go` - Index operations
- `rollingback.go` - Rollback logic for failed DDL
- `owner/` - Owner election and lease management

**Two-Phase Protocol**:
1. **Prepare**: Update metadata
2. **Execute**: Apply changes incrementally

---

### 10. Domain (`pkg/domain/`)

**Purpose**: Global metadata and lifecycle management

**Responsibilities**:
- InfoSchema (metadata cache) management
- Statistics handle
- DDL owner coordination
- Privilege manager
- Background workers (auto-analyze, TTL, distributed tasks)

**Key File**: `domain.go` (~2000 lines)

**Singleton Pattern**: One domain per TiDB instance

---

### 11. Statistics (`pkg/statistics/`)

**Purpose**: Table/index statistics for query optimization

**Components**:
- Histogram - Value distribution
- Count-Min Sketch - Cardinality estimation
- TopN - Most frequent values
- NDV (Number of Distinct Values)

**Collection**:
- Manual: `ANALYZE TABLE`
- Automatic: Background auto-analyze worker

**Usage**: Cost estimation in planner

---

### 12. Expression (`pkg/expression/`)

**Purpose**: Expression evaluation and built-in functions

**Categories**:
- Arithmetic: `+`, `-`, `*`, `/`, `%`
- Comparison: `=`, `<`, `>`, `LIKE`, `IN`
- Logical: `AND`, `OR`, `NOT`, `XOR`
- String functions: `CONCAT`, `SUBSTRING`, `UPPER`, etc.
- Date/time: `NOW()`, `DATE_ADD()`, etc.
- Aggregation: `SUM`, `AVG`, `COUNT`, `MAX`, `MIN`

**Evaluation Modes**:
- Row-based: Single row at a time
- Vectorized: Batch evaluation on chunks (performance optimization)

**Files**: 100+ files for different function categories

---

### 13. Types (`pkg/types/`)

**Purpose**: SQL type system implementation

**Supported Types**:
- Numeric: `INT`, `BIGINT`, `DECIMAL`, `FLOAT`, `DOUBLE`
- String: `CHAR`, `VARCHAR`, `TEXT`, `BLOB`
- Date/Time: `DATE`, `TIME`, `DATETIME`, `TIMESTAMP`
- JSON: Native JSON type
- Enum/Set: Enumeration and set types

**Key Operations**:
- Type conversion and coercion
- Collation handling
- Overflow checking
- NULL handling

---

### 14. Table Codec (`pkg/tablecodec/`)

**Purpose**: Encode SQL table data into key-value pairs

**Encoding Scheme**:
```
Table Record Key:   t{tableID}_r{rowID}
Table Record Value: [col1, col2, col3, ...]

Index Key:   t{tableID}_i{indexID}_{indexValue}
Index Value: {rowID}
```

**Features**:
- Efficient integer encoding (varint)
- Collation-aware string encoding
- NULL value handling
- Common handle support (primary key as handle)

---

### 15. Utilities (`pkg/util/`)

**Major Utilities**:
- `chunk/` - Columnar data container
- `codec/` - Encoding/decoding utilities
- `logutil/` - Logging infrastructure
- `memory/` - Memory tracking
- `disk/` - Temporary disk storage
- `ranger/` - Range calculation from conditions
- `tracing/` - Distributed tracing
- `topsql/` - Top SQL tracking
- `profile/` - Profiling utilities

---

## Testing Infrastructure

### Unit Tests (`pkg/*/`)
- Co-located with source code
- Pattern: `*_test.go`
- Framework: Standard `testing` + `testify`
- Custom: `pkg/testkit` for SQL testing

### Integration Tests (`tests/integrationtest/`)
```
tests/integrationtest/
├── t/                    # Test SQL files
│   ├── executor/
│   ├── planner/
│   ├── ddl/
│   └── expression/
├── r/                    # Expected results
│   └── [matching structure]
└── s/                    # Statistics JSON
    └── [matching structure]
```

**Format**:
```sql
-- test file: t/planner/core/point_get.test
select * from t where id = 1;

-- result file: r/planner/core/point_get.result
1 value1 value2
```

### Benchmark Tests
- Pattern: `*_benchmark_test.go`
- Uses `testing.B` for performance measurement
- Located alongside unit tests

---

## Build Configuration

### Bazel (`BUILD.bazel` files)
- Hermetic, reproducible builds
- Distributed in every package directory
- Supports remote caching and distributed execution

**Example**:
```python
go_test(
    name = "executor_test",
    srcs = glob(["*_test.go"]),
    shard_count = 50,  # Parallel test execution
    deps = [
        "//pkg/testkit",
        "@com_github_stretchr_testify//require",
    ],
)
```

### Make (`Makefile`)
- Developer-friendly interface
- Wraps Bazel and Go commands
- Targets: `make`, `make test`, `make dev`, `make check`

---

## Documentation (`docs/`)

**Design Documents**:
- `design/` - Architecture and feature design docs
- `tidb_http_api_list.md` - HTTP API reference

---

## Tools (`tools/`)

**Development Tools**:
- `check/` - Linting and validation scripts
- `bin/` - Compiled tool binaries

---

## External Tools

### BR (Backup & Restore) (`br/`)
- Distributed backup to S3/GCS/local
- Point-in-time recovery
- Incremental backup support

### Dumpling (`dumpling/`)
- MySQL-compatible export tool
- Parallel export for performance
- Multiple output formats (SQL, CSV)

### Lightning (`lightning/`)
- High-performance data import
- Direct KV pair writing to TiKV (bypasses SQL layer)
- Use case: Initial data load

---

## Configuration Files

- `go.mod` - Go module dependencies
- `.golangci.yml` - Linter configuration
- `.github/` - GitHub Actions workflows
- `OWNERS` - Code ownership and review assignments

---

## File Count Summary

| Category | Count | Purpose |
|----------|-------|---------|
| Go source files | 3,908 | Main codebase |
| Go test files | ~1,500 | Unit and integration tests |
| Bazel BUILD files | ~500 | Build configuration |
| TOML config files | ~50 | Configuration schemas |

---

## Growth Areas (by LOC)

1. **Executor** (~180K lines) - Most complex subsystem
2. **Planner** (~120K lines) - Optimization complexity
3. **Parser** (~50K lines) - SQL syntax coverage
4. **Expression** (~40K lines) - Built-in function library
5. **Session** (~25K lines) - State management

---

## Recommended Reading Path for New Contributors

1. Start: `cmd/tidb-server/main.go` - Understand initialization
2. Protocol: `pkg/server/conn.go` - See how SQL arrives
3. Parsing: `pkg/parser/parser.go` - AST generation
4. Planning: `pkg/planner/core/optimizer.go` - Optimization pipeline
5. Execution: `pkg/executor/adapter.go` - Execution coordination
6. Storage: `pkg/kv/kv.go` - Storage abstractions

---

**Next**: See [Dependency Graph](./dependency-graph.md) for component relationships.
