package store

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"errors"
	"net/url"
	"path/filepath"

	_ "github.com/mattn/go-sqlite3"
)

type Store struct{ db *sql.DB }

type User struct {
	ID                int
	Login, Name, Role string
}

var ErrLogin = errors.New("invalid credentials")

func Open(path string) (*Store, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	dsn := url.URL{Scheme: "file", Path: absolute, RawQuery: "_foreign_keys=on&_busy_timeout=5000&_txlock=immediate"}
	db, err := sql.Open("sqlite3", dsn.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema + assignmentSchema + moduleSchema + finalSchema); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func passwordHash(password string, salt []byte) []byte {
	hash, err := pbkdf2.Key(sha256.New, password, salt, 600000, 32)
	if err != nil {
		panic(err)
	}
	return hash
}

func (s *Store) Seed() error {
	accounts := []struct{ login, name, role, password string }{
		{"mentor", "Анна Соколова", "mentor", "Mentor-demo-26"},
		{"employee", "Иван Петров", "employee", "Employee-demo-26"},
		{"employee2", "Мария Волкова", "employee", "Employee-demo-26"},
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, a := range accounts {
		salt := make([]byte, 16)
		rand.Read(salt)
		_, err := tx.Exec(`INSERT INTO users(login,name,role,salt,hash)
			VALUES(?,?,?,?,?) ON CONFLICT(login) DO NOTHING`,
			a.login, a.name, a.role, salt, passwordHash(a.password, salt))
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) Login(login, password string) (User, error) {
	var user User
	var salt, hash []byte
	err := s.db.QueryRow(`SELECT id,login,name,role,salt,hash FROM users WHERE login=?`, login).
		Scan(&user.ID, &user.Login, &user.Name, &user.Role, &salt, &hash)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return User{}, err
	}
	if subtle.ConstantTimeCompare(passwordHash(password, salt), hash) != 1 {
		return User{}, ErrLogin
	}
	return user, nil
}

const schema = `
CREATE TABLE IF NOT EXISTS users (
	id INTEGER PRIMARY KEY,
	login TEXT NOT NULL UNIQUE,
	name TEXT NOT NULL,
	role TEXT NOT NULL CHECK(role IN ('mentor','employee')),
	salt BLOB NOT NULL,
	hash BLOB NOT NULL
);
`
