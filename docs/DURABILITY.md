# Durability

StunDB's durable wrapper combines an in-memory B+-tree (see ARCHITECTURE.md) with a
write-ahead log. The tree itself is not stored as pages on disk. Instead, the
WAL records mutations and recovery rebuilds memory by replaying those records.

## WAL File

Each WAL file starts with a fixed header:

```text
[Magic:4][Version:4]
```

The current magic value is `WAL1`, stored as `0x57414C31`, and the current
version is `1`.

Each log entry is encoded as:

```text
[length:4][sequence:8][op:1][keyLen:4][key][valueLen:4][value][checksum:4]
```

The checksum is CRC32-IEEE over the entry sequence, operation, key, and value.
The supported operations are insert, delete, and clear.

## Write Path

`DurableBTree` logs a mutation before applying it to the in-memory tree:

```text
append WAL entry -> flush according to SyncMode -> update memory
```

`BulkInsert` appends all insert entries, syncs the WAL, and then applies the
batch to the tree with its batched insert.

## Sync Modes

`SyncBatch` is the zero value and default. It flushes and fsyncs every
`BatchSize` entries.

`SyncNone` flushes buffered bytes but does not call fsync for each entry.

`SyncAlways` flushes and fsyncs every entry.

## Recovery

Opening a `DurableBTree` validates the WAL header, scans entries to find the
latest sequence number, and replays every valid entry into a new in-memory
tree.

If a corrupted tail is found while scanning entries, replay stops at the last
valid entry.

## Checkpoint

`Checkpoint` flushes the WAL and truncates it back to a fresh header. In this
codebase the B-Tree still lives only in memory; there is no separate persisted
tree file created by checkpointing.
