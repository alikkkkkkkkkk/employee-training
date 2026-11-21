package store

import (
	"database/sql"
	"errors"

	"employee-training/internal/training"
)

const moduleSchema = `
CREATE TABLE IF NOT EXISTS module_progress (
	employee_id INTEGER NOT NULL REFERENCES assignments(employee_id),
	module INTEGER NOT NULL CHECK(module BETWEEN 1 AND 3),
	last_score INTEGER CHECK(last_score BETWEEN 0 AND 5),
	PRIMARY KEY(employee_id, module)
);
`

var (
	ErrLocked = errors.New("training step is locked")
	ErrUnread = errors.New("material has not been confirmed")
)

type ModuleProgress struct {
	Number    int
	LastScore sql.NullInt64
}

func (p ModuleProgress) Passed() bool {
	return p.LastScore.Valid && p.LastScore.Int64 >= training.ModulePass
}

type Progress struct {
	Final    FinalResult
	Assigned bool
	Modules  []ModuleProgress
}

func (p Progress) Passed() int {
	count := 0
	for _, module := range p.Modules {
		if module.Passed() {
			count++
		}
	}
	return count
}

func (p Progress) FinalOpen() bool {
	return p.Assigned && p.Passed() == len(training.Modules)
}

func (p Progress) Module(number int) (ModuleProgress, bool) {
	for _, module := range p.Modules {
		if module.Number == number {
			return module, true
		}
	}
	return ModuleProgress{}, false
}

func (p Progress) Allows(number int) bool {
	return p.Assigned && number >= 1 && number <= len(training.Modules) && number <= p.Passed()+1
}

func (s *Store) Progress(employeeID int) (Progress, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return Progress{}, err
	}
	defer tx.Rollback()
	return loadProgress(tx, employeeID)
}

func loadProgress(db *sql.Tx, employeeID int) (Progress, error) {
	var p Progress
	err := db.QueryRow(`SELECT EXISTS(SELECT 1 FROM assignments WHERE employee_id=?)`, employeeID).Scan(&p.Assigned)
	if err != nil || !p.Assigned {
		return p, err
	}
	rows, err := db.Query(`SELECT module,last_score FROM module_progress WHERE employee_id=? ORDER BY module`, employeeID)
	if err != nil {
		return p, err
	}
	defer rows.Close()
	for rows.Next() {
		var module ModuleProgress
		if err := rows.Scan(&module.Number, &module.LastScore); err != nil {
			return p, err
		}
		p.Modules = append(p.Modules, module)
	}
	if err := rows.Err(); err != nil {
		return p, err
	}
	p.Final, err = loadFinal(db, employeeID)
	return p, err
}

func (s *Store) ConfirmRead(employeeID, number int) error {
	return s.updateModule(employeeID, number, func(tx *sql.Tx, _ bool) error {
		_, err := tx.Exec(`INSERT INTO module_progress(employee_id,module) VALUES(?,?)
			ON CONFLICT(employee_id,module) DO NOTHING`, employeeID, number)
		return err
	})
}

func (s *Store) RecordModule(employeeID, number, score int) error {
	return s.updateModule(employeeID, number, func(tx *sql.Tx, read bool) error {
		if !read {
			return ErrUnread
		}
		_, err := tx.Exec(`UPDATE module_progress SET last_score=? WHERE employee_id=? AND module=?`,
			score, employeeID, number)
		return err
	})
}

func (s *Store) updateModule(employeeID, number int, update func(*sql.Tx, bool) error) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	p, err := loadProgress(tx, employeeID)
	if err != nil {
		return err
	}
	if !p.Allows(number) {
		return ErrLocked
	}
	module, read := p.Module(number)
	if module.Passed() {
		return nil
	}
	if err := update(tx, read); err != nil {
		return err
	}
	return tx.Commit()
}
