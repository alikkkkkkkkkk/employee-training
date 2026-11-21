package store

import (
	"database/sql"
	"errors"
)

const assignmentSchema = `
CREATE TABLE IF NOT EXISTS assignments (
	employee_id INTEGER PRIMARY KEY REFERENCES users(id)
);
`

var ErrEmployee = errors.New("unknown employee")

type Assignment struct {
	Employee User
	Assigned bool
}

func (s *Store) Assigned(employeeID int) (bool, error) {
	var assigned bool
	err := s.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM assignments WHERE employee_id=?)`, employeeID).Scan(&assigned)
	return assigned, err
}

func (s *Store) Assign(employeeID int) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var id int
	err = tx.QueryRow(`SELECT id FROM users WHERE id=? AND role='employee'`, employeeID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrEmployee
	}
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO assignments(employee_id) VALUES(?) ON CONFLICT(employee_id) DO NOTHING`, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) Employees() ([]Assignment, error) {
	rows, err := s.db.Query(`
		SELECT u.id, u.login, u.name, a.employee_id IS NOT NULL
		FROM users u LEFT JOIN assignments a ON a.employee_id=u.id
		WHERE u.role='employee' ORDER BY u.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var employees []Assignment
	for rows.Next() {
		var row Assignment
		err := rows.Scan(&row.Employee.ID, &row.Employee.Login, &row.Employee.Name, &row.Assigned)
		if err != nil {
			return nil, err
		}
		employees = append(employees, row)
	}
	return employees, rows.Err()
}
