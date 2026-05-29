package storage

import (
	"database/sql"

	_ "modernc.org/sqlite"
)

// Open returns the writer pool: a single connection so all writes serialize
// (the issue-number allocator and the claim CAS rely on this) and never race
// each other into SQLITE_BUSY. WAL lets OpenRead's readers run concurrently
// against it.
func Open(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	db.SetMaxOpenConns(1)
	return db, nil
}

// readPoolSize caps concurrent read connections. WAL allows many readers
// alongside the single writer; this bounds fan-out so a polling fleet of
// agents doesn't queue reads behind one another (or behind a write).
const readPoolSize = 4

// OpenRead returns a read-only pool against the same database file. query_only
// rejects accidental writes defensively. Call after Open has put the file in
// WAL mode. Reads here don't queue behind the writer, which is the point.
func OpenRead(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=query_only(1)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	db.SetMaxOpenConns(readPoolSize)
	return db, nil
}
