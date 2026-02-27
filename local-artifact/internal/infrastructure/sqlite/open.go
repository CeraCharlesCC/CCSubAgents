package sqlite

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

func OpenMetaDB(workspaceRoot string) (*sql.DB, error) {
	if err := os.MkdirAll(workspaceRoot, 0o755); err != nil {
		return nil, err
	}
	return openSQLite(filepath.Join(workspaceRoot, "meta.sqlite"), metaSchemaV1, 1)
}

func OpenRegistryDB(baseRoot string) (*sql.DB, error) {
	if err := os.MkdirAll(baseRoot, 0o755); err != nil {
		return nil, err
	}
	return openSQLite(filepath.Join(baseRoot, "registry.sqlite"), registrySchemaV1, 1)
}

func openSQLite(path string, schema string, targetUserVersion int) (*sql.DB, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(ON)&_pragma=busy_timeout(5000)&_txlock=immediate", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}

	var userVersion int
	if err := db.QueryRow("PRAGMA user_version;").Scan(&userVersion); err != nil {
		_ = db.Close()
		return nil, err
	}
	if userVersion == 0 {
		if _, err := db.Exec(schema); err != nil {
			_ = db.Close()
			return nil, err
		}
		if _, err := db.Exec(fmt.Sprintf("PRAGMA user_version = %d;", targetUserVersion)); err != nil {
			_ = db.Close()
			return nil, err
		}
	} else if userVersion != targetUserVersion {
		_ = db.Close()
		return nil, fmt.Errorf("unsupported user_version %d for %s", userVersion, path)
	}

	return db, nil
}
