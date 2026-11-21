package web

import (
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"employee-training/internal/store"
)

type browser struct {
	t      *testing.T
	client *http.Client
	base   string
	csrf   string
}

type handlerTransport struct{ handler http.Handler }

func (h handlerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	recorder := httptest.NewRecorder()
	h.handler.ServeHTTP(recorder, r)
	return recorder.Result(), nil
}

func newBrowser(t *testing.T, handler http.Handler) *browser {
	t.Helper()
	jar, _ := cookiejar.New(nil)
	return &browser{t: t, base: "http://training.local", client: &http.Client{Jar: jar, Transport: handlerTransport{handler},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

var csrfField = regexp.MustCompile(`name="csrf" value="([^"]+)"`)

func (b *browser) request(path string, data url.Values, status int, fragments ...string) string {
	b.t.Helper()
	var resp *http.Response
	var err error
	if data == nil {
		resp, err = b.client.Get(b.base + path)
	} else {
		if !data.Has("csrf") {
			data.Set("csrf", b.csrf)
		}
		resp, err = b.client.PostForm(b.base+path, data)
	}
	if err != nil {
		b.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		b.t.Fatal(err)
	}
	body := string(raw)
	if resp.StatusCode != status {
		b.t.Fatalf("%s: status %d, want %d; body: %s", path, resp.StatusCode, status, body)
	}
	for _, fragment := range fragments {
		if !strings.Contains(body, fragment) {
			b.t.Fatalf("%s: missing %q in %s", path, fragment, body)
		}
	}
	if match := csrfField.FindStringSubmatch(body); match != nil {
		b.csrf = match[1]
	}
	return body
}

func (b *browser) login(login, password, role string) {
	b.t.Helper()
	b.request("/", nil, 200, "Войти")
	b.request("/login", url.Values{"login": {login}, "password": {password}}, 303)
	b.request("/"+role, nil, 200)
}

func openDemo(t *testing.T, path string) *store.Store {
	t.Helper()
	db, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Seed(); err != nil {
		db.Close()
		t.Fatal(err)
	}
	return db
}

func TestSessionAndRoles(t *testing.T) {
	db := openDemo(t, filepath.Join(t.TempDir(), "training.db"))
	defer db.Close()
	b := newBrowser(t, New(db))
	b.request("/mentor", nil, 303)
	b.request("/login", url.Values{}, 403)
	b.request("/", nil, 200)
	b.request("/login", url.Values{"login": {"employee"}, "password": {"wrong"}}, 200, "Проверьте логин")
	b.login("employee", "Employee-demo-26", "employee")
	b.request("/mentor", nil, 403)
	b.request("/logout", url.Values{"csrf": {"invalid"}}, 403)
	b.request("/logout", url.Values{}, 303)
	b.request("/employee", nil, 303)
	b.login("mentor", "Mentor-demo-26", "mentor")
	b.request("/employee", nil, 403)
}

func TestAssignment(t *testing.T) {
	path := filepath.Join(t.TempDir(), "training.db")
	db := openDemo(t, path)
	handler := New(db)
	mentor, employee := newBrowser(t, handler), newBrowser(t, handler)
	mentor.login("mentor", "Mentor-demo-26", "mentor")
	employee.login("employee", "Employee-demo-26", "employee")
	employee.request("/employee", nil, 200, "Вам пока не назначен курс")
	employee.request("/mentor/assign", url.Values{"employee": {"2"}}, 403)
	mentor.request("/mentor/assign", url.Values{"employee": {"1"}}, 404)
	for range 2 {
		mentor.request("/mentor/assign", url.Values{"employee": {"2"}}, 303)
	}
	body := mentor.request("/mentor", nil, 200, "Назначенные сотрудники")
	if strings.Count(body, "<p>Иван Петров</p>") != 1 {
		t.Fatal("expected one assigned employee")
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	employee = newBrowser(t, New(db))
	employee.login("employee", "Employee-demo-26", "employee")
	employee.request("/employee", nil, 200, "Курс назначен")
}
