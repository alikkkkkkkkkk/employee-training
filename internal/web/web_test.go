package web

import (
	"fmt"
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
	if strings.Count(body, "<td>Иван Петров</td>") != 1 {
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

func answers(scope string, choices ...int) url.Values {
	form := make(url.Values)
	for index, choice := range choices {
		form.Set(fmt.Sprintf("%s-q%d", scope, index+1), fmt.Sprint(choice))
	}
	return form
}

func TestModuleProgress(t *testing.T) {
	db := openDemo(t, filepath.Join(t.TempDir(), "training.db"))
	defer db.Close()
	handler := New(db)
	mentor, employee, other := newBrowser(t, handler), newBrowser(t, handler), newBrowser(t, handler)
	mentor.login("mentor", "Mentor-demo-26", "mentor")
	employee.login("employee", "Employee-demo-26", "employee")
	other.login("employee2", "Employee-demo-26", "employee")
	mentor.request("/mentor/assign", url.Values{"employee": {"2"}}, 303)
	mentor.request("/modules/1", nil, 403)
	other.request("/modules/1?employee=2", nil, 403)
	employee.request("/modules/2", nil, 403)
	employee.request("/modules/2/read", url.Values{}, 403)
	employee.request("/modules/2/quiz", answers("2", 2, 0, 1, 1, 2), 403)
	employee.request("/modules/1/quiz", answers("1", 1, 0, 2, 1, 0), 403)
	employee.request("/modules/1/read", url.Values{}, 303)
	employee.request("/modules/1/quiz", answers("1", 1, 0, 2, 1), 400)
	employee.request("/modules/1/quiz", answers("2", 1, 0, 2, 1, 0), 400)
	employee.request("/modules/1/quiz", answers("1", 1, 0, 2, 0, 1), 303)
	employee.request("/modules/1", nil, 200, "Результат: 3 из 5")
	employee.request("/modules/2", nil, 403)
	employee.request("/modules/1/quiz", answers("1", 1, 0, 2, 1, 1), 303)
	employee.request("/modules/1", nil, 200, "Результат: 4 из 5")
	employee.request("/modules/2", nil, 200, "Модуль 2")
	employee.request("/modules/3", nil, 403)
	mentor.request("/mentor/assign", url.Values{"employee": {"2"}}, 303)
	mentor.request("/mentor", nil, 200, "<td>Иван Петров</td><td>1 из 3</td>")
	employee.request("/logout", url.Values{}, 303)
	employee.login("employee", "Employee-demo-26", "employee")
	employee.request("/employee", nil, 200, "Пройдено модулей: 1 из 3", `href="/modules/2">Продолжить`)
}

var submissionField = regexp.MustCompile(`name="submission" value="([^"]+)"`)

func finalForm(b *browser, choices ...int) url.Values {
	b.t.Helper()
	body := b.request("/final", nil, 200, "Итоговый тест")
	match := submissionField.FindStringSubmatch(body)
	if match == nil {
		b.t.Fatal("missing final exam form")
	}
	form := answers("final", choices...)
	form.Set("submission", match[1])
	form.Set("csrf", b.csrf)
	return form
}

func passModules(b *browser) {
	b.t.Helper()
	choices := [][]int{{1, 0, 2, 1, 0}, {2, 0, 1, 1, 2}, {0, 1, 2, 0, 1}}
	for index, choice := range choices {
		number := fmt.Sprint(index + 1)
		b.request("/modules/"+number+"/read", url.Values{}, 303)
		b.request("/modules/"+number+"/quiz", answers(number, choice...), 303)
	}
}

func TestCourseEndToEnd(t *testing.T) {
	path := filepath.Join(t.TempDir(), "training.db")
	db := openDemo(t, path)
	defer func() { db.Close() }()
	handler := New(db)
	mentor, employee := newBrowser(t, handler), newBrowser(t, handler)
	mentor.login("mentor", "Mentor-demo-26", "mentor")
	mentor.request("/mentor/assign", url.Values{"employee": {"2"}}, 303)
	mentor.request("/mentor", nil, 200, "<td>Нет попытки</td><td>Нет попытки</td>")
	employee.login("employee", "Employee-demo-26", "employee")
	employee.request("/final", nil, 403)
	passModules(employee)
	poor := finalForm(employee, 1, 0, 2, 1, 0, 2, 1, 1, 0, 0) // 7/10
	employee.request("/final", poor, 303)
	employee.request("/final", poor, 303)
	employee.request("/final", nil, 200, "Первый результат: 7 из 10", "Осталось попыток: 2")
	success := finalForm(employee, 1, 0, 2, 1, 0, 2, 1, 0, 0, 0) // 8/10
	incomplete := url.Values{}
	for key, value := range success {
		incomplete[key] = value
	}
	incomplete.Del("final-q10")
	employee.request("/final", incomplete, 400)
	employee.request("/final", success, 303)
	employee.request("/final", success, 303)
	employee.request("/final", nil, 200, "Курс завершён", "Лучший результат: 8 из 10", "Осталось попыток: 1")
	mentor.request("/mentor/assign", url.Values{"employee": {"2"}}, 303)
	mentor.request("/mentor", nil, 200, "<td>Иван Петров</td><td>3 из 3</td>",
		"<td>Курс завершён</td><td>7 из 10</td><td>8 из 10</td>")
	employee.request("/mentor", nil, 403)
	employee.request("/logout", url.Values{}, 303)

	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	db = reopened
	handler = New(db)
	employee = newBrowser(t, handler)
	employee.login("employee", "Employee-demo-26", "employee")
	employee.request("/employee", nil, 200, "Пройдено модулей: 3 из 3", "Курс завершён")
	employee.request("/final", nil, 200, "Первый результат: 7 из 10", "Лучший результат: 8 из 10")
	other := newBrowser(t, handler)
	other.login("employee2", "Employee-demo-26", "employee")
	other.request("/final?employee=2", nil, 403)
}

func TestFinalConcurrentLimit(t *testing.T) {
	db := openDemo(t, filepath.Join(t.TempDir(), "training.db"))
	defer db.Close()
	handler := New(db)
	mentor, employee := newBrowser(t, handler), newBrowser(t, handler)
	mentor.login("mentor", "Mentor-demo-26", "mentor")
	mentor.request("/mentor/assign", url.Values{"employee": {"2"}}, 303)
	employee.login("employee", "Employee-demo-26", "employee")
	passModules(employee)

	wrong := []int{0, 1, 0, 0, 1, 0, 0, 1, 0, 0}
	first := finalForm(employee, wrong...)
	submitPair := func(a, b url.Values) map[int]int {
		t.Helper()
		execute := func(form url.Values, results chan<- int) {
			resp, err := employee.client.PostForm(employee.base+"/final", form)
			if err != nil {
				t.Error(err)
				results <- 0
				return
			}
			resp.Body.Close()
			results <- resp.StatusCode
		}
		results := make(chan int, 2)
		go execute(a, results)
		go execute(b, results)
		counts := make(map[int]int)
		counts[<-results]++
		counts[<-results]++
		return counts
	}
	if statuses := submitPair(first, first); statuses[303] != 2 {
		t.Fatalf("concurrent duplicate: %v", statuses)
	}
	employee.request("/final", nil, 200, "Осталось попыток: 2")
	employee.request("/final", finalForm(employee, wrong...), 303)
	third, fourth := finalForm(employee, wrong...), finalForm(employee, wrong...)
	if statuses := submitPair(third, fourth); statuses[303] != 1 || statuses[409] != 1 {
		t.Fatalf("concurrent final slot: %v", statuses)
	}
	employee.request("/final", nil, 200, "попытки исчерпаны", "Осталось попыток: 0")
}
