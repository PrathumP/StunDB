package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/prathump/stundb/internal/index"
	"github.com/prathump/stundb/internal/wal"
)

func main() {
	dir, err := os.MkdirTemp("", "stundb-demo-*")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(dir)

	db, err := wal.NewDurableBTree(wal.DurableConfig{
		WALPath: filepath.Join(dir, "stundb.wal"),
	})
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	records := []struct {
		key   wal.Keytype
		value wal.Valuetype
	}{
		{wal.Keytype("user:1"), wal.Valuetype(`{"email":"alice@example.com","age":30}`)},
		{wal.Keytype("user:2"), wal.Valuetype(`{"email":"bob@example.com","age":24}`)},
		{wal.Keytype("user:3"), wal.Valuetype(`{"email":"carol@example.com","age":35}`)},
		{wal.Keytype("user:4"), wal.Valuetype(`{"email":"dinesh@example.com","age":28}`)},
		{wal.Keytype("user:5"), wal.Valuetype(`{"email":"eve@example.com","age":41}`)},
	}

	for _, record := range records {
		if err := db.Insert(record.key, record.value); err != nil {
			log.Fatal(err)
		}
	}

	value, err := db.Find(wal.Keytype("user:3"))
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Find user:3 => %s\n", value)

	keys, values, err := db.GetRange(wal.Keytype("user:2"), wal.Keytype("user:4"))
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("Range user:2..user:4")
	for i := range keys {
		fmt.Printf("  %s => %s\n", keys[i], values[i])
	}

	indexed := index.NewIndexedBTree(index.IndexedConfig{})
	if err := indexed.CreateIndex("email", index.JSONFieldExtractor("email"), true); err != nil {
		log.Fatal(err)
	}
	for _, record := range records {
		if err := indexed.Insert(index.Keytype(record.key), index.Valuetype(record.value)); err != nil {
			log.Fatal(err)
		}
	}

	primaryKey, err := indexed.FindByIndex("email", []byte("alice@example.com"))
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Index email=alice@example.com => %s\n", primaryKey)
	fmt.Printf("WAL path: %s\n", db.WALPath())
}
