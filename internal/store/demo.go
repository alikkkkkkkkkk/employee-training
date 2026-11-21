package store

import "employee-training/internal/training"

// SeedShowcase prepares the second employee for a short demo on a fresh database.
func (s *Store) SeedShowcase() error {
	if err := s.Seed(); err != nil {
		return err
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var employeeID int
	if err := tx.QueryRow(`SELECT id FROM users WHERE login='employee2' AND role='employee'`).Scan(&employeeID); err != nil {
		return err
	}
	result, err := tx.Exec(`INSERT INTO assignments(employee_id) VALUES(?) ON CONFLICT(employee_id) DO NOTHING`, employeeID)
	if err != nil {
		return err
	}
	added, err := result.RowsAffected()
	if err != nil || added == 0 {
		return err
	}
	for index, module := range training.Modules {
		if _, err := tx.Exec(`INSERT INTO module_progress(employee_id,module,last_score) VALUES(?,?,?)`,
			employeeID, index+1, len(module.Questions)); err != nil {
			return err
		}
	}
	return tx.Commit()
}
