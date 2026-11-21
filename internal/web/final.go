package web

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"

	"employee-training/internal/store"
	"employee-training/internal/training"
)

func signSubmission(key, nonce string) []byte {
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(nonce))
	return mac.Sum(nil)
}

func (a *app) final(w http.ResponseWriter, r *http.Request, s session) {
	p, err := a.store.Progress(s.user.ID)
	if err != nil {
		fail(w, err)
		return
	}
	if !p.FinalOpen() {
		learningError(w, store.ErrLocked)
		return
	}
	v := view{User: s.user, CSRF: s.csrf, Progress: p, AttemptsLeft: training.FinalLimit - p.Final.Attempts}
	if !p.Final.Completed() && !p.Final.Exhausted() {
		nonce := rand.Text()
		v.Submission = nonce + "." + hex.EncodeToString(signSubmission(s.csrf, nonce))
		v.Questions = quizQuestions("final", training.Final)
	}
	render(w, "final", v)
}

func (a *app) submitFinal(w http.ResponseWriter, r *http.Request, s session) {
	nonce, tag, ok := strings.Cut(r.PostForm.Get("submission"), ".")
	signature, err := hex.DecodeString(tag)
	if !ok || nonce == "" || err != nil || !hmac.Equal(signature, signSubmission(s.csrf, nonce)) {
		http.Error(w, "Недействительная форма итогового теста", http.StatusBadRequest)
		return
	}
	score, err := quizScore("final", training.Final, r.PostForm)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := a.store.RecordFinal(s.user.ID, nonce, score); err != nil {
		learningError(w, err)
		return
	}
	http.Redirect(w, r, "/final", http.StatusSeeOther)
}
