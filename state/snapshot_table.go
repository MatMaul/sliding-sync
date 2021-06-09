package state

import (
	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"
)

type SnapshotRow struct {
	SnapshotID int           `db:"snapshot_id"`
	RoomID     string        `db:"room_id"`
	Events     pq.Int64Array `db:"events"`
}

// SnapshotTable stores room state snapshots. Each snapshot has a unique numeric ID.
// Not every event will be associated with a snapshot.
type SnapshotTable struct {
}

func NewSnapshotsTable(db *sqlx.DB) *SnapshotTable {
	// make sure tables are made
	db.MustExec(`
	CREATE SEQUENCE IF NOT EXISTS syncv3_snapshots_seq;
	CREATE TABLE IF NOT EXISTS syncv3_snapshots (
		snapshot_id BIGINT PRIMARY KEY DEFAULT nextval('syncv3_snapshots_seq'),
		room_id TEXT NOT NULL,
		events BIGINT[] NOT NULL
	);
	`)
	return &SnapshotTable{}
}

// Select a row based on its snapshot ID.
func (s *SnapshotTable) Select(txn *sqlx.Tx, snapshotID int) (row SnapshotRow, err error) {
	err = txn.Get(&row, `SELECT * FROM syncv3_snapshots WHERE snapshot_id = $1`, snapshotID)
	return
}

// Insert the row. Modifies SnapshotID to be the inserted primary key.
func (s *SnapshotTable) Insert(txn *sqlx.Tx, row *SnapshotRow) error {
	var id int
	err := txn.QueryRow(`INSERT INTO syncv3_snapshots(room_id, events) VALUES($1, $2) RETURNING snapshot_id`, row.RoomID, row.Events).Scan(&id)
	row.SnapshotID = id
	return err
}

// Delete the snapshot IDs given
func (s *SnapshotTable) Delete(txn *sqlx.Tx, snapshotIDs []int) error {
	query, args, err := sqlx.In(`DELETE FROM syncv3_snapshots WHERE snapshot_id IN (?)`, snapshotIDs)
	if err != nil {
		return err
	}
	query = txn.Rebind(query)
	_, err = txn.Exec(query, args...)
	return err
}