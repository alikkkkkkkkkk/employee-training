package web

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"employee-training/internal/store"
	"employee-training/internal/training"
)

type moduleLink struct {
	Number       int
	Title        string
	Open, Passed bool
}

type quizQuestion struct {
	Name, Text string
	Options    [3]string
}

func quizQuestions(scope string, questions []training.Question) []quizQuestion {
	result := make([]quizQuestion, len(questions))
	for i, q := range questions {
		result[i] = quizQuestion{fmt.Sprintf("%s-q%d", scope, i+1), q.Text, q.Options}
	}
	return result
}

func quizScore(scope string, questions []training.Question, form url.Values) (int, error) {
	answers := make([]int, len(questions))
	for i, question := range quizQuestions(scope, questions) {
		values := form[question.Name]
		if len(values) != 1 {
			return 0, errors.New("ответьте на все вопросы")
		}
		answer, err := strconv.Atoi(values[0])
		if err != nil {
			return 0, errors.New("выберите допустимый вариант ответа")
		}
		answers[i] = answer
	}
	return training.Score(questions, answers)
}

func (a *app) employee(w http.ResponseWriter, r *http.Request, s session) {
	p, err := a.store.Progress(s.user.ID)
	if err != nil {
		fail(w, err)
		return
	}
	v := view{User: s.user, CSRF: s.csrf, Progress: p, TotalModules: len(training.Modules)}
	for i, module := range training.Modules {
		number := i + 1
		progress, _ := p.Module(number)
		v.Modules = append(v.Modules, moduleLink{number, module.Title, p.Allows(number), progress.Passed()})
	}
	render(w, "employee", v)
}

func moduleNumber(w http.ResponseWriter, r *http.Request) int {
	number, err := strconv.Atoi(r.PathValue("number"))
	if err != nil || number < 1 || number > len(training.Modules) {
		http.NotFound(w, r)
		return 0
	}
	return number
}

func (a *app) module(w http.ResponseWriter, r *http.Request, s session) {
	number := moduleNumber(w, r)
	if number == 0 {
		return
	}
	p, err := a.store.Progress(s.user.ID)
	if err != nil {
		fail(w, err)
		return
	}
	if !p.Allows(number) {
		learningError(w, store.ErrLocked)
		return
	}
	module := training.Modules[number-1]
	progress, read := p.Module(number)
	render(w, "module", view{User: s.user, CSRF: s.csrf, Number: number, Module: module,
		Read: read, ModuleProgress: progress, Questions: quizQuestions(fmt.Sprint(number), module.Questions)})
}

func (a *app) confirmRead(w http.ResponseWriter, r *http.Request, s session) {
	number := moduleNumber(w, r)
	if number == 0 {
		return
	}
	if err := a.store.ConfirmRead(s.user.ID, number); err != nil {
		learningError(w, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/modules/%d", number), http.StatusSeeOther)
}

func (a *app) submitModule(w http.ResponseWriter, r *http.Request, s session) {
	number := moduleNumber(w, r)
	if number == 0 {
		return
	}
	score, err := quizScore(fmt.Sprint(number), training.Modules[number-1].Questions, r.PostForm)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := a.store.RecordModule(s.user.ID, number, score); err != nil {
		learningError(w, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/modules/%d", number), http.StatusSeeOther)
}

func learningError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrLocked):
		http.Error(w, "Этот шаг пока закрыт", http.StatusForbidden)
	case errors.Is(err, store.ErrUnread):
		http.Error(w, "Сначала подтвердите прочтение материала", http.StatusForbidden)
	case errors.Is(err, store.ErrFinalLimit):
		http.Error(w, "Все три попытки использованы", http.StatusConflict)
	case errors.Is(err, store.ErrCompleted):
		http.Error(w, "Курс уже завершён", http.StatusConflict)
	default:
		fail(w, err)
	}
}
