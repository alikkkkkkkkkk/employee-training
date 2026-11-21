package web

import (
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"errors"
	"html/template"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"employee-training/internal/store"
)

//go:embed templates/*
var files embed.FS

var pages = template.Must(template.ParseFS(files, "templates/*.html"))

const sessionCookie = "training_session"

type session struct {
	id, csrf string
	user     store.User
	expires  time.Time
}

type app struct {
	store    *store.Store
	mu       sync.Mutex
	sessions map[string]session
}

type view struct {
	User      store.User
	CSRF      string
	Message   string
	Assigned  bool
	Employees []store.Assignment
}

func New(db *store.Store) http.Handler {
	a := &app{store: db, sessions: make(map[string]session)}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", a.home)
	mux.HandleFunc("POST /login", a.login)
	mux.HandleFunc("POST /logout", a.require("", a.logout))
	mux.HandleFunc("GET /employee", a.require("employee", a.employee))
	mux.HandleFunc("GET /mentor", a.require("mentor", a.mentor))
	mux.HandleFunc("POST /mentor/assign", a.require("mentor", a.assign))
	protected := http.NewCrossOriginProtection().Handler(mux)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		protected.ServeHTTP(w, r)
	})
}

func (a *app) current(r *http.Request) session {
	cookie, err := r.Cookie(sessionCookie)
	if err != nil {
		return session{}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	s := a.sessions[cookie.Value]
	if time.Now().Before(s.expires) {
		return s
	}
	return session{}
}

func (a *app) start(w http.ResponseWriter, r *http.Request, previous session, user store.User) session {
	now := time.Now()
	s := session{id: rand.Text(), csrf: rand.Text(), user: user, expires: now.Add(8 * time.Hour)}
	a.mu.Lock()
	for id, old := range a.sessions {
		if !now.Before(old.expires) {
			delete(a.sessions, id)
		}
	}
	delete(a.sessions, previous.id)
	a.sessions[s.id] = s
	a.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: s.id, Path: "/", HttpOnly: true,
		SameSite: http.SameSiteLaxMode, Secure: r.TLS != nil, Expires: s.expires})
	return s
}

func parseForm(w http.ResponseWriter, r *http.Request, s session) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Некорректная форма", http.StatusBadRequest)
		return false
	}
	if s.csrf == "" || subtle.ConstantTimeCompare([]byte(s.csrf), []byte(r.PostForm.Get("csrf"))) != 1 {
		http.Error(w, "Недействительный токен формы. Обновите страницу.", http.StatusForbidden)
		return false
	}
	return true
}

func (a *app) require(role string, next func(http.ResponseWriter, *http.Request, session)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s := a.current(r)
		if s.user.ID == 0 {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		if role != "" && s.user.Role != role {
			http.Error(w, "Доступ запрещён", http.StatusForbidden)
			return
		}
		if r.Method == http.MethodPost && !parseForm(w, r, s) {
			return
		}
		next(w, r, s)
	}
}

func (a *app) home(w http.ResponseWriter, r *http.Request) {
	s := a.current(r)
	if s.user.ID != 0 {
		http.Redirect(w, r, "/"+s.user.Role, http.StatusSeeOther)
		return
	}
	if s.id == "" {
		s = a.start(w, r, s, store.User{})
	}
	render(w, "home", view{CSRF: s.csrf})
}

func (a *app) login(w http.ResponseWriter, r *http.Request) {
	s := a.current(r)
	if !parseForm(w, r, s) {
		return
	}
	user, err := a.store.Login(strings.TrimSpace(r.PostForm.Get("login")), r.PostForm.Get("password"))
	if errors.Is(err, store.ErrLogin) {
		render(w, "home", view{CSRF: s.csrf, Message: "Проверьте логин и пароль."})
		return
	}
	if err != nil {
		fail(w, err)
		return
	}
	a.start(w, r, s, user)
	http.Redirect(w, r, "/"+user.Role, http.StatusSeeOther)
}

func (a *app) logout(w http.ResponseWriter, r *http.Request, s session) {
	a.mu.Lock()
	delete(a.sessions, s.id)
	a.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Path: "/", MaxAge: -1, HttpOnly: true,
		SameSite: http.SameSiteLaxMode, Secure: r.TLS != nil})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (a *app) employee(w http.ResponseWriter, r *http.Request, s session) {
	assigned, err := a.store.Assigned(s.user.ID)
	if err != nil {
		fail(w, err)
		return
	}
	render(w, "employee", view{User: s.user, CSRF: s.csrf, Assigned: assigned})
}

func (a *app) mentor(w http.ResponseWriter, r *http.Request, s session) {
	employees, err := a.store.Employees()
	if err != nil {
		fail(w, err)
		return
	}
	render(w, "mentor", view{User: s.user, CSRF: s.csrf, Employees: employees})
}

func (a *app) assign(w http.ResponseWriter, r *http.Request, s session) {
	id, err := strconv.Atoi(r.PostForm.Get("employee"))
	if err != nil {
		http.Error(w, "Выберите сотрудника", http.StatusBadRequest)
		return
	}
	err = a.store.Assign(id)
	if errors.Is(err, store.ErrEmployee) {
		http.Error(w, "Сотрудник не найден", http.StatusNotFound)
		return
	}
	if err != nil {
		fail(w, err)
		return
	}
	http.Redirect(w, r, "/mentor", http.StatusSeeOther)
}

func fail(w http.ResponseWriter, err error) {
	log.Print(err)
	http.Error(w, "Ошибка приложения", http.StatusInternalServerError)
}

func render(w http.ResponseWriter, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := pages.ExecuteTemplate(w, name, data); err != nil {
		log.Print(err)
	}
}
