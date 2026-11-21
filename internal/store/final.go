package store

import (
	"database/sql"
	"errors"

	"employee-training/internal/training"
)

const finalSchema = `
CREATE TABLE IF NOT EXISTS final_attempts (
	employee_id INTEGER NOT NULL REFERENCES assignments(employee_id),
	attempt INTEGER NOT NULL CHECK(attempt BETWEEN 1 AND 3),
	submission TEXT NOT NULL,
	score INTEGER NOT NULL CHECK(score BETWEEN 0 AND 10),
	PRIMARY KEY(employee_id, attempt),
	UNIQUE(employee_id, submission)
);
`

var (
	ErrFinalLimit = errors.New("final exam attempt limit reached")
	ErrCompleted  = errors.New("course already completed")
)

type FinalResult struct {
	Attempts    int
	First, Best sql.NullInt64
}

func (r FinalResult) Completed() bool {
	return r.Best.Valid && r.Best.Int64 >= training.FinalPass
}

func (r FinalResult) Exhausted() bool {
	return r.Attempts >= training.FinalLimit && !r.Completed()
}

const finalSummary = `
	SELECT COUNT(*), MAX(CASE WHEN attempt=1 THEN score END), MAX(score)
	FROM final_attempts WHERE employee_id=?`

func loadFinal(tx *sql.Tx, employeeID int) (FinalResult, error) {
	var result FinalResult
	err := tx.QueryRow(finalSummary, employeeID).Scan(&result.Attempts, &result.First, &result.Best)
	return result, err
}

func (s *Store) RecordFinal(employeeID int, submission string, score int) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	p, err := loadProgress(tx, employeeID)
	if err != nil {
		return err
	}
	if !p.FinalOpen() {
		return ErrLocked
	}
	var recorded bool
	err = tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM final_attempts WHERE employee_id=? AND submission=?)`,
		employeeID, submission).Scan(&recorded)
	if err != nil || recorded {
		return err
	}
	if p.Final.Completed() {
		return ErrCompleted
	}
	if p.Final.Exhausted() {
		return ErrFinalLimit
	}
	// Completion is derived from these immutable scores, so it changes in the same commit.
	if _, err := tx.Exec(`INSERT INTO final_attempts(employee_id,attempt,submission,score) VALUES(?,?,?,?)`,
		employeeID, p.Final.Attempts+1, submission, score); err != nil {
		return err
	}
	return tx.Commit()
}
