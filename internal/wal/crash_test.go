package wal

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// Real-crash versions of the WAL repros: a child process (this test binary,
// re-executed) writes to the WAL and is SIGKILLed mid-write.

const crashEnv = "STUNDB_ADV_CRASH_CHILD"

// TestAdvCrashChild is the child half; it does nothing in a normal run.
func TestAdvCrashChild(t *testing.T) {
	mode := os.Getenv(crashEnv)
	if mode == "" {
		t.Skip("child helper")
	}
	path := os.Getenv("STUNDB_ADV_WAL")
	db, err := NewDurableBTree(DurableConfig{WALPath: path, SyncMode: SyncAlways})
	if err != nil {
		panic(err)
	}
	switch mode {
	case "bulk":
		const n = 2_000_000
		keys := make([]Keytype, n)
		vals := make([]Valuetype, n)
		for i := range keys {
			keys[i] = []byte(fmt.Sprintf("bulk%08d", i))
			vals[i] = []byte("value-value-value")
		}
		os.WriteFile(path+".ready", nil, 0o644)
		db.BulkInsert(keys, vals) // killed in here
	case "big":
		db.Insert([]byte("a"), []byte("v-a"))
		db.Insert([]byte("b"), []byte("v-b"))
		big := bytes.Repeat([]byte("x"), 256<<20)
		os.WriteFile(path+".ready", nil, 0o644)
		db.Insert([]byte("c"), big) // killed in here
	}
	time.Sleep(time.Hour)
}

func spawnAndKill(t *testing.T, mode, path string, killAt int64) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestAdvCrashChild$", "-test.count=1")
	cmd.Env = append(os.Environ(), crashEnv+"="+mode, "STUNDB_ADV_WAL="+path)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path + ".ready"); err == nil {
			if fi, err := os.Stat(path); err == nil && fi.Size() > killAt {
				break
			}
		}
		time.Sleep(50 * time.Microsecond)
	}
	cmd.Process.Kill()
	cmd.Wait()
	fi, _ := os.Stat(path)
	t.Logf("child %q SIGKILLed with WAL at %d bytes", mode, fi.Size())
}

// TestAdvKillDuringBulkInsert: BulkInsert claims "All entries are logged
// before any are applied (atomic batch)". A crash while it is logging leaves a
// prefix of the batch in the WAL, and recovery applies that prefix.
func TestAdvKillDuringBulkInsert(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bulk.wal")
	spawnAndKill(t, "bulk", path, 4<<20)
	db, err := NewDurableBTree(DurableConfig{WALPath: path})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	t.Logf("after restart: Count = %d of a 2000000-key BulkInsert that never returned", db.Count())
	if c := db.Count(); c != 0 && c != 2_000_000 {
		t.Errorf("recovered a partial BulkInsert batch: %d of 2000000 keys", c)
	}
}

// TestAdvKillDuringLargeWriteThenAck: SIGKILL during a large Insert leaves a
// torn entry. After a restart, a write acknowledged with SyncAlways is lost
// on the following restart.
func TestAdvKillDuringLargeWriteThenAck(t *testing.T) {
	for attempt := 0; attempt < 5; attempt++ {
		path := filepath.Join(t.TempDir(), "big.wal")
		spawnAndKill(t, "big", path, 1<<20)
		db, err := NewDurableBTree(DurableConfig{WALPath: path, SyncMode: SyncAlways})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Find([]byte("c")); err == nil {
			t.Logf("attempt %d: kill landed after the big write completed; retrying", attempt)
			db.Close()
			continue
		}
		t.Logf("restart 1: Count = %d (a, b; the torn c is dropped)", db.Count())
		if err := db.Insert([]byte("d"), []byte("v-d")); err != nil {
			t.Fatal(err)
		}
		v, _ := db.Find([]byte("d"))
		t.Logf("restart 1: Insert(d) acknowledged (SyncAlways); Find(d) = %q", v)
		db.Close()

		db, err = NewDurableBTree(DurableConfig{WALPath: path, SyncMode: SyncAlways})
		if err != nil {
			t.Fatal(err)
		}
		v, err = db.Find([]byte("d"))
		t.Logf("restart 2: Count = %d, Find(d) = %q, %v", db.Count(), v, err)
		db.Close()
		if err != nil {
			t.Errorf("acknowledged write d lost after a crash + two restarts: %v", err)
		}
		return
	}
	t.Skip("could not land the kill inside the large write")
}
