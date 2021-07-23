package state

import (
	"encoding/json"

	"github.com/jmoiron/sqlx"
)

type Storage struct {
	accumulator *Accumulator
	typingTable *TypingTable
}

func NewStorage(postgresURI string) *Storage {
	db, err := sqlx.Open("postgres", postgresURI)
	if err != nil {
		log.Panic().Err(err).Str("uri", postgresURI).Msg("failed to open SQL DB")
	}
	acc := &Accumulator{
		db:                    db,
		roomsTable:            NewRoomsTable(db),
		eventsTable:           NewEventTable(db),
		snapshotTable:         NewSnapshotsTable(db),
		snapshotRefCountTable: NewSnapshotRefCountsTable(db),
		membershipLogTable:    NewMembershipLogTable(db),
		entityName:            "server",
	}
	return &Storage{
		accumulator: acc,
		typingTable: NewTypingTable(db),
	}
}

func (s *Storage) LatestEventNID() (int64, error) {
	return s.accumulator.eventsTable.SelectHighestNID()
}

func (s *Storage) LatestTypingID() (int64, error) {
	return s.typingTable.SelectHighestID()
}

func (s *Storage) Accumulate(roomID string, timeline []json.RawMessage) (int, error) {
	return s.accumulator.Accumulate(roomID, timeline)
}

func (s *Storage) Initialise(roomID string, state []json.RawMessage) (bool, error) {
	return s.accumulator.Initialise(roomID, state)
}

func (s *Storage) AllJoinedMembers() (map[string][]string, error) {
	// TODO
	_, err := s.accumulator.snapshotTable.CurrentSnapshots()
	if err != nil {
		return nil, err
	}
	return nil, nil
}

// Typing returns who is currently typing in this room along with the latest stream ID.
func (s *Storage) Typing(roomID string, fromStreamIDExcl int64) ([]string, int64, error) {
	return s.typingTable.Typing(roomID, fromStreamIDExcl)
}

// SetTyping sets who is typing in the room. An empty list removes all typing users. Returns the
// stream ID of the newly inserted typing users.
func (s *Storage) SetTyping(roomID string, userIDs []string) (int64, error) {
	return s.typingTable.SetTyping(roomID, userIDs)
}
