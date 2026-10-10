// Package httpapi 是门口。网页把请求送到这里，这里再叫仓库去存取数据。
package httpapi

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"

	"qiutu/internal/model"
	"qiutu/internal/store"
)

// API 把仓库和页面目录包在一起。
type API struct {
	Store  *store.Store
	WebDir string
}

func New(st *store.Store, webDir string) *API {
	return &API{Store: st, WebDir: webDir}
}

// Handler 登记全部地址。地址和产品文档第 9 节对应。
func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", a.health)
	mux.HandleFunc("GET /api/modules", a.modules)
	mux.HandleFunc("GET /api/modules/{id}/actions", a.moduleActions)
	mux.HandleFunc("GET /api/actions/{id}", a.action)
	mux.HandleFunc("GET /api/users/{id}/progress", a.progress)
	mux.HandleFunc("GET /api/users/{id}/history", a.history)
	mux.HandleFunc("GET /api/workout/sessions/{id}", a.session)
	mux.HandleFunc("POST /api/workout/sessions", a.createSession)
	mux.HandleFunc("POST /api/workout/sets", a.addSet)
	mux.HandleFunc("POST /api/workout/finish", a.finish)
	mux.HandleFunc("POST /api/symptoms", a.symptom)
	mux.HandleFunc("POST /api/upgrade/check", a.check)
	mux.HandleFunc("POST /api/upgrade/confirm", a.confirm)
	mux.HandleFunc("POST /api/rollback", a.rollback)
	mux.HandleFunc("GET /api/stats", a.stats)

	mux.HandleFunc("GET /api/admin/modules", a.adminOnly(a.adminModules))
	mux.HandleFunc("GET /api/admin/actions", a.adminOnly(a.adminActions))
	mux.HandleFunc("PUT /api/admin/actions/{id}", a.adminOnly(a.adminUpdateAction))
	mux.HandleFunc("GET /api/admin/users", a.adminOnly(a.adminUsers))
	mux.HandleFunc("GET /api/admin/users/{id}/progress", a.adminOnly(a.adminProgress))
	mux.HandleFunc("POST /api/admin/users/{id}/lock", a.adminOnly(a.adminLock))
	mux.HandleFunc("POST /api/admin/users/{id}/rollback", a.adminOnly(a.adminRollback))

	mux.Handle("GET /", http.FileServer(http.Dir(a.WebDir)))
	return mux
}

func (a *API) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *API) modules(w http.ResponseWriter, r *http.Request) {
	userID, ok := queryUser(w, r)
	if !ok {
		return
	}
	home, err := a.Store.Home(userID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, home)
}

func (a *API) moduleActions(w http.ResponseWriter, r *http.Request) {
	userID, ok := queryUser(w, r)
	if !ok {
		return
	}
	moduleID, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	detail, err := a.Store.ModuleDetail(userID, moduleID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (a *API) action(w http.ResponseWriter, r *http.Request) {
	userID, ok := queryUser(w, r)
	if !ok {
		return
	}
	actionID, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	detail, err := a.Store.ActionDetail(userID, actionID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (a *API) progress(w http.ResponseWriter, r *http.Request) {
	userID, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	home, err := a.Store.Home(userID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"modules": home.Modules})
}

func (a *API) history(w http.ResponseWriter, r *http.Request) {
	userID, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	list, err := a.Store.History(userID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": list})
}

func (a *API) session(w http.ResponseWriter, r *http.Request) {
	sessionID, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	session, sets, action, err := a.Store.SessionBundle(sessionID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"session": session, "sets": sets, "action": action})
}

func (a *API) createSession(w http.ResponseWriter, r *http.Request) {
	var body struct {
		UserID   int `json:"user_id"`
		ModuleID int `json:"module_id"`
		ActionID int `json:"action_id"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	if body.UserID == 0 {
		body.UserID = 1
	}
	session, err := a.Store.CreateSession(body.UserID, body.ModuleID, body.ActionID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, session)
}

func (a *API) addSet(w http.ResponseWriter, r *http.Request) {
	var body store.AddSetInput
	if !readJSON(w, r, &body) {
		return
	}
	result, err := a.Store.AddSet(body)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (a *API) finish(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SessionID int    `json:"session_id"`
		Status    string `json:"status"`
		Note      string `json:"note"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	result, err := a.Store.Finish(body.SessionID, body.Status, body.Note)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (a *API) symptom(w http.ResponseWriter, r *http.Request) {
	var body struct {
		UserID      int    `json:"user_id"`
		BodyPart    string `json:"body_part"`
		SymptomType string `json:"symptom_type"`
		Severity    int    `json:"severity"`
		Note        string `json:"note"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	if body.UserID == 0 {
		body.UserID = 1
	}
	item, err := a.Store.AddSymptom(body.UserID, body.BodyPart, body.SymptomType, body.Severity, body.Note)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (a *API) check(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SessionID int `json:"session_id"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	result, err := a.Store.CheckUpgrade(body.SessionID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (a *API) confirm(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SessionID int `json:"session_id"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	progress, err := a.Store.ConfirmUpgrade(body.SessionID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, progress)
}

func (a *API) rollback(w http.ResponseWriter, r *http.Request) {
	var body struct {
		UserID    int    `json:"user_id"`
		ModuleID  int    `json:"module_id"`
		Reason    string `json:"reason"`
		SessionID int    `json:"session_id"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	if body.UserID == 0 {
		body.UserID = 1
	}
	progress, err := a.Store.Rollback(body.UserID, body.ModuleID, body.Reason, body.SessionID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, progress)
}

func (a *API) stats(w http.ResponseWriter, r *http.Request) {
	userID, ok := queryUser(w, r)
	if !ok {
		return
	}
	stats, err := a.Store.Stats(userID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

func (a *API) adminModules(w http.ResponseWriter, _ *http.Request) {
	home, err := a.Store.Home(1)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"modules": home.Modules})
}

func (a *API) adminActions(w http.ResponseWriter, r *http.Request) {
	moduleID, err := strconv.Atoi(r.URL.Query().Get("module_id"))
	if err != nil || moduleID <= 0 {
		writeErr(w, http.StatusBadRequest, "请带上模块编号")
		return
	}
	list, err := a.Store.ActionsOf(moduleID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"actions": list})
}

func (a *API) adminUpdateAction(w http.ResponseWriter, r *http.Request) {
	actionID, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var body model.Action
	if !readJSON(w, r, &body) {
		return
	}
	body.ID = actionID
	if err := a.Store.UpdateAction(body); err != nil {
		writeStoreErr(w, err)
		return
	}
	action, err := a.Store.ActionDetail(1, actionID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, action.Action)
}

func (a *API) adminUsers(w http.ResponseWriter, _ *http.Request) {
	users, err := a.Store.Users()
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": users})
}

func (a *API) adminProgress(w http.ResponseWriter, r *http.Request) {
	a.progress(w, r)
}

func (a *API) adminLock(w http.ResponseWriter, r *http.Request) {
	userID, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var body struct {
		ModuleID int    `json:"module_id"`
		Level    int    `json:"level"`
		Locked   bool   `json:"locked"`
		Reason   string `json:"reason"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	if err := a.Store.Lock(userID, body.ModuleID, body.Level, body.Locked, body.Reason); err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *API) adminRollback(w http.ResponseWriter, r *http.Request) {
	userID, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var body struct {
		ModuleID int    `json:"module_id"`
		Reason   string `json:"reason"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	progress, err := a.Store.Rollback(userID, body.ModuleID, body.Reason, 0)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, progress)
}

// adminOnly 是演示用的门卫：请求头里带 X-Demo-Role: admin 才放行。
// 这不是真正的登录。正式上线必须换成账号和密码，否则谁都能改动作。
func (a *API) adminOnly(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Demo-Role") != "admin" {
			writeErr(w, http.StatusUnauthorized, "后台需要康复师或管理员身份。本地演示请从页面里的后台入口进入。")
			return
		}
		next(w, r)
	}
}

func queryUser(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := r.URL.Query().Get("user_id")
	if raw == "" {
		return 1, true
	}
	id, err := strconv.Atoi(raw)
	if err != nil || id <= 0 {
		writeErr(w, http.StatusBadRequest, "用户编号不正确")
		return 0, false
	}
	return id, true
}

func pathID(w http.ResponseWriter, r *http.Request, name string) (int, bool) {
	id, err := strconv.Atoi(r.PathValue(name))
	if err != nil || id <= 0 {
		writeErr(w, http.StatusBadRequest, "编号不正确")
		return 0, false
	}
	return id, true
}

func readJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		writeErr(w, http.StatusBadRequest, "请求内容看不懂，请检查填写的内容")
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, code int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("写出响应失败: %v", err)
	}
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func writeStoreErr(w http.ResponseWriter, err error) {
	var ue *store.UserError
	if errors.As(err, &ue) {
		code := http.StatusBadRequest
		if errors.Is(err, store.ErrNotFound) {
			code = http.StatusNotFound
		}
		if errors.Is(err, store.ErrLocked) {
			code = http.StatusForbidden
		}
		if errors.Is(err, store.ErrConflict) {
			code = http.StatusConflict
		}
		body := map[string]any{"error": ue.Msg}
		if ue.ActiveID > 0 {
			body["active_session_id"] = ue.ActiveID
		}
		writeJSON(w, code, body)
		return
	}
	log.Printf("服务器错误: %v", err)
	writeErr(w, http.StatusInternalServerError, "系统忙不过来，请稍后再试")
}
