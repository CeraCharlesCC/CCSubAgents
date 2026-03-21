package sqlite

import (
	"database/sql"
)

func closeDBIgnore(db *sql.DB) {
	if db == nil {
		return
	}
	if err := db.Close(); err != nil {
		_ = err
	}
}

func rollbackIgnore(tx *sql.Tx) {
	if tx == nil {
		return
	}
	if err := tx.Rollback(); err != nil {
		_ = err
	}
}

func closeRowsIgnore(rows *sql.Rows) {
	if rows == nil {
		return
	}
	if err := rows.Close(); err != nil {
		_ = err
	}
}
