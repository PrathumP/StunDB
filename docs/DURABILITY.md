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
The supported operations are insert, delete, clear, and batch. A batch entry
holds a whole `BulkInsert`: its value is

```text
[count:4] then count times [keyLen:4][key][valueLen:4][value]
```

## Write Path

`DurableBTree` logs a mutation before applying it to the in-memory tree:

```text
append WAL entry -> flush according to SyncMode -> update memory
```

`BulkInsert` appends the batch as one entry, syncs the WAL, and then applies
the batch to the tree with its batched insert. A batch is atomic twice over:

- **For recovery.** One entry has one checksum, so a crash while the batch is
  being written leaves a torn entry that recovery drops whole. Recovery never
  restores part of a batch.
- **For readers.** `Find`, `GetRange`, and `Count` see all of a batch or none
  of it. While a batch applies, a sequence counter is odd; those reads load it
  before and after reading and retry if a batch overlapped them. `ForEach` is
  not a snapshot and can see part of a batch.

Readers take no lock otherwise, so outside a batch a read costs two atomic
loads of a line only `BulkInsert` writes.

## Sync Modes

`SyncBatch` is the zero value and default. It flushes and fsyncs every
`BatchSize` entries.

`SyncNone` flushes buffered bytes but does not call fsync for each entry.

`SyncAlways` flushes and fsyncs every entry.

## Recovery

Opening a `DurableBTree` validates the WAL header, scans entries to find the
latest sequence number, and replays every valid entry into a new in-memory
tree.

A crash in the middle of an append leaves a torn entry at the end of the file.
Opening the WAL finds the end of the last whole entry and truncates the file
there, then syncs it. Without the truncation, the file is opened for appending,
so new entries would land after the torn one, and every later replay would
stop at the torn entry and never reach them: a write acknowledged after the
restart would be lost at the next one.

An entry counts as whole when its length field fits in the rest of the file,
its key and value lengths add up to that length, and its checksum matches. The
length checks also keep a damaged length field from making recovery allocate
gigabytes.

## Checkpoint

`Checkpoint` flushes the WAL and truncates it back to a fresh header. In this
codebase the B-Tree still lives only in memory; there is no separate persisted
tree file created by checkpointing.
