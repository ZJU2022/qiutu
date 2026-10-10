// Package store 是仓库：负责把数据放进数据库，再取出来。
// 页面不直接写 SQL。仓库取出数据后，交给 logic 做升级和安全判断。
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"qiutu/internal/logic"
	"qiutu/internal/model"
)

// Store 握着数据库连接。整个程序共用一个就够了。
type Store struct {
	DB *sql.DB
}

func New(db *sql.DB) *Store {
	return &Store{DB: db}
}

// UserError 是可以原样告诉用户的错误。Kind 用来决定网页状态码。
type UserError struct {
	Kind     error
	Msg      string
	ActiveID int
}

func (e *UserError) Error() string { return e.Msg }
func (e *UserError) Unwrap() error { return e.Kind }

var (
	ErrNotFound    = errors.New("not found")
	ErrLocked      = errors.New("locked")
	ErrConflict    = errors.New("conflict")
	ErrBadInput    = errors.New("bad input")
	ErrNotEligible = errors.New("not eligible")
)

func userErr(kind error, msg string) error {
	return &UserError{Kind: kind, Msg: msg}
}

// queryer 是“能查询的东西”。普通连接和事务都满足它，所以同一段查询两边都能用。
type queryer interface {
	Query(string, ...any) (*sql.Rows, error)
	QueryRow(string, ...any) *sql.Row
}

// AddSetInput 是页面提交“我做完一组”时带来的内容。
type AddSetInput struct {
	SessionID   int    `json:"session_id"`
	SetNo       int    `json:"set_no"`
	Reps        int    `json:"reps"`
	Seconds     int    `json:"seconds"`
	Side        string `json:"side"`
	RPE         int    `json:"rpe"`
	PainScore   int    `json:"pain_score"`
	SymptomType string `json:"symptom_type"`
	FormQuality string `json:"form_quality"`
}

var symptomTypes = map[string]bool{
	"无": true, "酸胀": true, "腰疼": true, "腿麻": true, "电流感": true, "刺痛": true, "其他": true,
}
var formTypes = map[string]bool{"标准": true, "代偿": true, "无法完成": true}
var sideTypes = map[string]bool{"无": true, "左": true, "右": true, "双侧": true}
var freqTypes = map[string]bool{"每天": true, "隔天": true, "每周2-3次": true, "仅康复中心": true, "自定义": true}

func (s *Store) Home(userID int) (model.Home, error) {
	if err := s.ensureUser(userID); err != nil {
		return model.Home{}, err
	}
	cards, activity, err := s.moduleCards(userID)
	if err != nil {
		return model.Home{}, err
	}
	active, err := s.activeSession(s.DB, userID)
	if err != nil {
		return model.Home{}, err
	}
	home := model.Home{
		SafetyTip:   "出现腰疼、腿麻、电流感、刺痛，立即停止。",
		Disclaimer:  "这不是诊断工具。练的时候不舒服，就停下来，听康复师的安排。",
		DailyLimit:  logic.DailyModuleLimit,
		TodayCount:  activity.todayCount,
		Suggestions: []model.Suggestion{},
		Modules:     cards,
		Active:      active,
	}
	if activity.todayCount >= logic.DailyModuleLimit {
		home.RestToday = true
		home.RestMessage = "今天已经练了 3 个模块，建议休息，避免过度训练。"
		return home, nil
	}
	for _, card := range cards {
		if len(home.Suggestions) >= logic.DailyModuleLimit-activity.todayCount {
			break
		}
		if card.Status != "进行中" || card.IsLocked || card.CurrentActionLocked || !card.DueToday {
			continue
		}
		if activity.trainedToday[card.ID] {
			continue
		}
		home.Suggestions = append(home.Suggestions, model.Suggestion{
			ModuleID:   card.ID,
			ModuleName: card.Name,
			ActionID:   card.CurrentActionID,
			ActionName: card.CurrentActionName,
			Level:      card.CurrentLevel,
			Reason:     "这是你这个模块当前要练的一级",
		})
	}
	return home, nil
}

func (s *Store) ModuleDetail(userID, moduleID int) (model.ModuleDetail, error) {
	cards, _, err := s.moduleCards(userID)
	if err != nil {
		return model.ModuleDetail{}, err
	}
	var card model.ModuleCard
	found := false
	for _, item := range cards {
		if item.ID == moduleID {
			card = item
			found = true
			break
		}
	}
	if !found {
		return model.ModuleDetail{}, userErr(ErrNotFound, "找不到这个模块")
	}
	rows, err := s.DB.Query(`SELECT id, module_id, level, name, target_sets, target_reps, target_seconds, target_side, frequency, points, contraindications, COALESCE(video_url,''), is_locked FROM actions WHERE module_id=? ORDER BY level`, moduleID)
	if err != nil {
		return model.ModuleDetail{}, err
	}
	defer rows.Close()
	actions := []model.Action{}
	for rows.Next() {
		action, err := scanAction(rows)
		if err != nil {
			return model.ModuleDetail{}, err
		}
		rawStatus := "进行中"
		if card.ClearedLevels >= 10 {
			rawStatus = "已完成"
		}
		action.State = actionState(action.Level, card.CurrentLevel, rawStatus, card.IsLocked, action.IsLocked)
		actions = append(actions, action)
	}
	return model.ModuleDetail{Module: card, Actions: actions}, rows.Err()
}

func (s *Store) ActionDetail(userID, actionID int) (model.ActionDetail, error) {
	action, err := s.actionByID(s.DB, actionID)
	if err != nil {
		return model.ActionDetail{}, err
	}
	progress, err := s.progressOf(s.DB, userID, action.ModuleID)
	if err != nil {
		return model.ActionDetail{}, err
	}
	var moduleLocked int
	if err := s.DB.QueryRow(`SELECT is_locked FROM modules WHERE id=?`, action.ModuleID).Scan(&moduleLocked); err != nil {
		return model.ActionDetail{}, err
	}
	history, err := s.history(s.DB, `WHERE s.user_id=? AND s.action_id=? AND s.status!='进行中'`, []any{userID, actionID}, 8)
	if err != nil {
		return model.ActionDetail{}, err
	}
	state := actionState(action.Level, progress.CurrentLevel, progress.Status, moduleLocked == 1, action.IsLocked)
	action.State = state
	detail := model.ActionDetail{Action: action, Progress: progress, History: history}
	switch state {
	case "current":
		detail.CanTrain = true
	case "locked":
		detail.LockHint = "已锁定，只能查看。解锁需要康复师操作。"
	case "future":
		detail.LockHint = "还没解锁。先把当前等级练到达标。"
	case "done":
		detail.LockHint = "这一级已经通过。"
	}
	if progress.Status == "已完成" && state != "locked" {
		detail.CanTrain = false
		detail.LockHint = "这个模块已经完成。"
	}
	return detail, nil
}

func (s *Store) CreateSession(userID, moduleID, actionID int) (model.Session, error) {
	tx, err := s.DB.Begin()
	if err != nil {
		return model.Session{}, err
	}
	defer func() { _ = tx.Rollback() }()

	if err := s.ensureUserTx(tx, userID); err != nil {
		return model.Session{}, err
	}
	var activeID, activeAction int
	err = tx.QueryRow(`SELECT id, action_id FROM workout_sessions WHERE user_id=? AND status='进行中' ORDER BY id DESC LIMIT 1`, userID).Scan(&activeID, &activeAction)
	if err == nil {
		if activeAction == actionID {
			if err := tx.Commit(); err != nil {
				return model.Session{}, err
			}
			return s.sessionByID(s.DB, activeID)
		}
		return model.Session{}, &UserError{Kind: ErrConflict, Msg: "你还有一次训练没结束。请先完成或结束它。", ActiveID: activeID}
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return model.Session{}, err
	}

	var level, actionLocked, moduleLocked, current int
	var status string
	err = tx.QueryRow(`SELECT a.level, a.is_locked, m.is_locked, p.current_level, p.status
		FROM actions a
		JOIN modules m ON m.id = a.module_id
		JOIN user_module_progress p ON p.module_id = a.module_id AND p.user_id = ?
		WHERE a.id=? AND a.module_id=?`, userID, actionID, moduleID).Scan(&level, &actionLocked, &moduleLocked, &current, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return model.Session{}, userErr(ErrNotFound, "找不到这个动作，或者这个用户还没有进度")
	}
	if err != nil {
		return model.Session{}, err
	}
	if moduleLocked == 1 || actionLocked == 1 || status == "已锁定" {
		return model.Session{}, userErr(ErrLocked, "这个动作已经锁定，只能查看，不能训练")
	}
	if status == "已完成" {
		return model.Session{}, userErr(ErrBadInput, "这个模块已经完成。如果要重练，请康复师先帮你回退")
	}
	if level != current {
		return model.Session{}, userErr(ErrBadInput, fmt.Sprintf("现在这个模块要练的是第 %d 级，不能跳级", current))
	}

	var otherToday int
	if err := tx.QueryRow(`SELECT COUNT(DISTINCT module_id) FROM workout_sessions WHERE user_id=? AND date=? AND total_sets>0 AND status!='跳过' AND module_id!=?`, userID, today(), moduleID).Scan(&otherToday); err != nil {
		return model.Session{}, err
	}
	if otherToday >= logic.DailyModuleLimit {
		return model.Session{}, userErr(ErrBadInput, "今天已经训练了 3 个模块，建议休息，避免过度训练")
	}

	res, err := tx.Exec(`INSERT INTO workout_sessions(user_id, module_id, action_id, date, status, total_sets, symptom_flag, note, created_at) VALUES (?, ?, ?, ?, '进行中', 0, 0, '', ?)`, userID, moduleID, actionID, today(), now())
	if err != nil {
		return model.Session{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return model.Session{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.Session{}, err
	}
	return s.sessionByID(s.DB, int(id))
}

func (s *Store) AddSet(in AddSetInput) (model.SetResult, error) {
	in, err := normalizeSet(in)
	if err != nil {
		return model.SetResult{}, err
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return model.SetResult{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var status string
	err = tx.QueryRow(`SELECT status FROM workout_sessions WHERE id=?`, in.SessionID).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return model.SetResult{}, userErr(ErrNotFound, "找不到这次训练")
	}
	if err != nil {
		return model.SetResult{}, err
	}
	if status != "进行中" {
		return model.SetResult{}, userErr(ErrBadInput, "这次训练已经结束，不能再记新的一组")
	}

	formOK := 0
	if in.FormQuality == "标准" {
		formOK = 1
	}
	res, err := tx.Exec(`INSERT INTO workout_set_logs(session_id, set_no, reps, seconds, side, rpe, pain_score, symptom_type, form_ok, form_quality, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		in.SessionID, in.SetNo, in.Reps, in.Seconds, in.Side, in.RPE, in.PainScore, in.SymptomType, formOK, in.FormQuality, now())
	if err != nil {
		return model.SetResult{}, err
	}
	setID, err := res.LastInsertId()
	if err != nil {
		return model.SetResult{}, err
	}
	warning := logic.WarningText(in.PainScore, in.SymptomType)
	flag := 0
	if warning != "" {
		flag = 1
	}
	if _, err := tx.Exec(`UPDATE workout_sessions SET total_sets=(SELECT COUNT(DISTINCT set_no) FROM workout_set_logs WHERE session_id=?), symptom_flag=CASE WHEN symptom_flag=1 OR ?=1 THEN 1 ELSE 0 END WHERE id=?`, in.SessionID, flag, in.SessionID); err != nil {
		return model.SetResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.SetResult{}, err
	}
	var set model.SetLog
	if err := s.DB.QueryRow(`SELECT id, session_id, set_no, reps, seconds, side, rpe, pain_score, symptom_type, form_ok, form_quality, created_at FROM workout_set_logs WHERE id=?`, setID).Scan(&set.ID, &set.SessionID, &set.SetNo, &set.Reps, &set.Seconds, &set.Side, &set.RPE, &set.PainScore, &set.SymptomType, &formOK, &set.FormQuality, &set.CreatedAt); err != nil {
		return model.SetResult{}, err
	}
	set.FormOK = formOK == 1
	return model.SetResult{Set: set, Warning: warning}, nil
}

func (s *Store) Finish(sessionID int, status, note string) (model.FinishResult, error) {
	if status == "" {
		status = "完成"
	}
	if status != "完成" && status != "中断" && status != "跳过" {
		return model.FinishResult{}, userErr(ErrBadInput, "训练状态只能是完成、中断或跳过")
	}
	note = trimRunes(note, 500)
	tx, err := s.DB.Begin()
	if err != nil {
		return model.FinishResult{}, err
	}
	defer func() { _ = tx.Rollback() }()

	current, err := s.sessionByID(tx, sessionID)
	if err != nil {
		return model.FinishResult{}, err
	}
	if current.Status == "进行中" {
		flag, err := s.symptomFlag(tx, sessionID)
		if err != nil {
			return model.FinishResult{}, err
		}
		if _, err := tx.Exec(`UPDATE workout_sessions SET status=?, note=?, symptom_flag=?, total_sets=(SELECT COUNT(DISTINCT set_no) FROM workout_set_logs WHERE session_id=?) WHERE id=?`, status, note, flag, sessionID, sessionID); err != nil {
			return model.FinishResult{}, err
		}
	}
	result, err := s.evaluate(tx, sessionID)
	if err != nil {
		return model.FinishResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.FinishResult{}, err
	}
	return result, nil
}

func (s *Store) CheckUpgrade(sessionID int) (model.FinishResult, error) {
	return s.evaluate(s.DB, sessionID)
}

func (s *Store) ConfirmUpgrade(sessionID int) (model.Progress, error) {
	tx, err := s.DB.Begin()
	if err != nil {
		return model.Progress{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var already int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM upgrade_logs WHERE source_session_id=? AND type='升级'`, sessionID).Scan(&already); err != nil {
		return model.Progress{}, err
	}
	session, err := s.sessionByID(tx, sessionID)
	if err != nil {
		return model.Progress{}, err
	}
	progress, err := s.progressOf(tx, session.UserID, session.ModuleID)
	if err != nil {
		return model.Progress{}, err
	}
	if already > 0 {
		progress.Already = true
		if err := tx.Commit(); err != nil {
			return model.Progress{}, err
		}
		return progress, nil
	}
	view, err := s.upgradeView(tx, sessionID)
	if err != nil {
		return model.Progress{}, err
	}
	if !view.Eligible {
		return model.Progress{}, userErr(ErrNotEligible, "这次训练还没有达到升级标准")
	}
	if progress.CurrentLevel != session.Level {
		return model.Progress{}, userErr(ErrBadInput, "你已经不在这个等级，不能用同一次训练重复升级")
	}
	if progress.Status == "已完成" {
		progress.Already = true
		if err := tx.Commit(); err != nil {
			return model.Progress{}, err
		}
		return progress, nil
	}

	to := progress.CurrentLevel
	newStatus := "已完成"
	reason := "完成该模块最高级"
	if progress.CurrentLevel < 10 {
		to = progress.CurrentLevel + 1
		newStatus = "进行中"
		reason = fmt.Sprintf("达到升级标准：达标 %d 组", view.GoodSets)
	}
	maxLevel := progress.MaxLevel
	if to > maxLevel {
		maxLevel = to
	}
	if _, err := tx.Exec(`UPDATE user_module_progress SET current_level=?, max_level=?, status=?, updated_at=? WHERE user_id=? AND module_id=?`, to, maxLevel, newStatus, now(), session.UserID, session.ModuleID); err != nil {
		return model.Progress{}, err
	}
	if _, err := tx.Exec(`INSERT INTO upgrade_logs(user_id, module_id, action_id, from_level, to_level, type, reason, source_session_id, created_at) VALUES (?, ?, ?, ?, ?, '升级', ?, ?, ?)`, session.UserID, session.ModuleID, session.ActionID, progress.CurrentLevel, to, reason, sessionID, now()); err != nil {
		return model.Progress{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.Progress{}, err
	}
	updated, err := s.progressOf(s.DB, session.UserID, session.ModuleID)
	return updated, err
}

func (s *Store) Rollback(userID, moduleID int, reason string, sessionID int) (model.Progress, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return model.Progress{}, userErr(ErrBadInput, "请写下回退原因")
	}
	reason = trimRunes(reason, 300)
	tx, err := s.DB.Begin()
	if err != nil {
		return model.Progress{}, err
	}
	defer func() { _ = tx.Rollback() }()

	progress, err := s.progressOf(tx, userID, moduleID)
	if err != nil {
		return model.Progress{}, err
	}
	if progress.CurrentLevel <= 1 {
		return model.Progress{}, userErr(ErrBadInput, "已经在第 1 级，不能再回退。请先暂停并联系康复师")
	}
	var actionID int
	if err := tx.QueryRow(`SELECT id FROM actions WHERE module_id=? AND level=?`, moduleID, progress.CurrentLevel).Scan(&actionID); err != nil {
		return model.Progress{}, err
	}
	to := progress.CurrentLevel - 1
	if _, err := tx.Exec(`UPDATE user_module_progress SET current_level=?, status='进行中', updated_at=? WHERE user_id=? AND module_id=?`, to, now(), userID, moduleID); err != nil {
		return model.Progress{}, err
	}
	var src any
	if sessionID > 0 {
		src = sessionID
	}
	if _, err := tx.Exec(`INSERT INTO upgrade_logs(user_id, module_id, action_id, from_level, to_level, type, reason, source_session_id, created_at) VALUES (?, ?, ?, ?, ?, '回退', ?, ?, ?)`, userID, moduleID, actionID, progress.CurrentLevel, to, reason, src, now()); err != nil {
		return model.Progress{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.Progress{}, err
	}
	return s.progressOf(s.DB, userID, moduleID)
}

func (s *Store) AddSymptom(userID int, bodyPart, symptomType string, severity int, note string) (model.Symptom, error) {
	if err := s.ensureUser(userID); err != nil {
		return model.Symptom{}, err
	}
	bodyPart = strings.TrimSpace(bodyPart)
	symptomType = strings.TrimSpace(symptomType)
	if bodyPart == "" {
		return model.Symptom{}, userErr(ErrBadInput, "请选择不舒服的部位")
	}
	if !symptomTypes[symptomType] || symptomType == "无" {
		return model.Symptom{}, userErr(ErrBadInput, "请选择症状类型")
	}
	if severity < 0 || severity > 10 {
		return model.Symptom{}, userErr(ErrBadInput, "严重程度要在 0 到 10 之间")
	}
	note = trimRunes(strings.TrimSpace(note), 500)
	stamp := now()
	res, err := s.DB.Exec(`INSERT INTO symptom_logs(user_id, date, body_part, symptom_type, severity, note, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`, userID, stamp, bodyPart, symptomType, severity, note, stamp)
	if err != nil {
		return model.Symptom{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return model.Symptom{}, err
	}
	return model.Symptom{ID: int(id), UserID: userID, Date: stamp, BodyPart: bodyPart, SymptomType: symptomType, Severity: severity, Note: note, CreatedAt: stamp}, nil
}

func (s *Store) Stats(userID int) (model.Stats, error) {
	if err := s.ensureUser(userID); err != nil {
		return model.Stats{}, err
	}
	stats := model.Stats{Days: []model.DayStat{}, Symptoms: []model.Symptom{}, Events: []model.UpgradeLog{}}
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM workout_sessions WHERE user_id=? AND status='完成'`, userID).Scan(&stats.TotalSessions); err != nil {
		return model.Stats{}, err
	}
	if err := s.DB.QueryRow(`SELECT COALESCE(SUM(total_sets),0) FROM workout_sessions WHERE user_id=?`, userID).Scan(&stats.TotalSets); err != nil {
		return model.Stats{}, err
	}
	if err := s.DB.QueryRow(`SELECT COALESCE(SUM(l.reps),0), COALESCE(SUM(l.seconds),0) FROM workout_set_logs l JOIN workout_sessions s ON s.id=l.session_id WHERE s.user_id=?`, userID).Scan(&stats.TotalReps, &stats.TotalSeconds); err != nil {
		return model.Stats{}, err
	}
	rows, err := s.DB.Query(`SELECT date, COUNT(*) FROM workout_sessions WHERE user_id=? AND total_sets>0 GROUP BY date ORDER BY date ASC LIMIT 60`, userID)
	if err != nil {
		return model.Stats{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var day model.DayStat
		if err := rows.Scan(&day.Date, &day.Sessions); err != nil {
			return model.Stats{}, err
		}
		stats.Days = append(stats.Days, day)
	}
	if err := rows.Err(); err != nil {
		return model.Stats{}, err
	}
	symRows, err := s.DB.Query(`SELECT id, user_id, date, body_part, symptom_type, severity, COALESCE(note,''), created_at FROM symptom_logs WHERE user_id=? ORDER BY id DESC LIMIT 20`, userID)
	if err != nil {
		return model.Stats{}, err
	}
	defer symRows.Close()
	for symRows.Next() {
		var item model.Symptom
		if err := symRows.Scan(&item.ID, &item.UserID, &item.Date, &item.BodyPart, &item.SymptomType, &item.Severity, &item.Note, &item.CreatedAt); err != nil {
			return model.Stats{}, err
		}
		stats.Symptoms = append(stats.Symptoms, item)
	}
	evRows, err := s.DB.Query(`SELECT l.id, l.user_id, l.module_id, m.name, l.action_id, l.from_level, l.to_level, l.type, COALESCE(l.reason,''), l.created_at FROM upgrade_logs l JOIN modules m ON m.id=l.module_id WHERE l.user_id=? ORDER BY l.id DESC LIMIT 30`, userID)
	if err != nil {
		return model.Stats{}, err
	}
	defer evRows.Close()
	for evRows.Next() {
		var item model.UpgradeLog
		if err := evRows.Scan(&item.ID, &item.UserID, &item.ModuleID, &item.ModuleName, &item.ActionID, &item.FromLevel, &item.ToLevel, &item.Type, &item.Reason, &item.CreatedAt); err != nil {
			return model.Stats{}, err
		}
		stats.Events = append(stats.Events, item)
	}
	cards, _, err := s.moduleCards(userID)
	if err != nil {
		return model.Stats{}, err
	}
	stats.Modules = cards
	return stats, nil
}

func (s *Store) History(userID int) ([]model.Session, error) {
	if err := s.ensureUser(userID); err != nil {
		return nil, err
	}
	return s.history(s.DB, `WHERE s.user_id=?`, []any{userID}, 50)
}

func (s *Store) SessionBundle(sessionID int) (model.Session, []model.SetLog, model.Action, error) {
	session, err := s.sessionByID(s.DB, sessionID)
	if err != nil {
		return model.Session{}, nil, model.Action{}, err
	}
	sets, err := s.setsOf(s.DB, sessionID)
	if err != nil {
		return model.Session{}, nil, model.Action{}, err
	}
	action, err := s.actionByID(s.DB, session.ActionID)
	if err != nil {
		return model.Session{}, nil, model.Action{}, err
	}
	return session, sets, action, nil
}

func (s *Store) Users() ([]model.User, error) {
	rows, err := s.DB.Query(`SELECT id, name, role FROM users ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	users := []model.User{}
	for rows.Next() {
		var user model.User
		if err := rows.Scan(&user.ID, &user.Name, &user.Role); err != nil {
			return nil, err
		}
		users = append(users, user)
	}
	return users, rows.Err()
}

func (s *Store) UpdateAction(action model.Action) error {
	action.Name = strings.TrimSpace(action.Name)
	if action.Name == "" {
		return userErr(ErrBadInput, "动作名称不能为空")
	}
	if action.TargetSets < 1 || action.TargetSets > 20 {
		return userErr(ErrBadInput, "目标组数要在 1 到 20 之间")
	}
	if action.TargetReps != nil && (*action.TargetReps < 1 || *action.TargetReps > 500) {
		return userErr(ErrBadInput, "目标次数要在 1 到 500 之间，不想限次数就留空")
	}
	if action.TargetSeconds != nil && (*action.TargetSeconds < 1 || *action.TargetSeconds > 7200) {
		return userErr(ErrBadInput, "目标秒数要在 1 到 7200 之间，不想限时间就留空")
	}
	if !sideTypes[action.TargetSide] {
		return userErr(ErrBadInput, "左右侧只能是无、左、右或双侧")
	}
	if !freqTypes[action.Frequency] {
		return userErr(ErrBadInput, "频率不在可选范围内")
	}
	action.Points = trimRunes(action.Points, 2000)
	action.Contraindications = trimRunes(action.Contraindications, 2000)
	action.VideoURL = trimRunes(strings.TrimSpace(action.VideoURL), 500)
	res, err := s.DB.Exec(`UPDATE actions SET name=?, target_sets=?, target_reps=?, target_seconds=?, target_side=?, frequency=?, points=?, contraindications=?, video_url=?, is_locked=? WHERE id=?`,
		action.Name, action.TargetSets, nullInt(action.TargetReps), nullInt(action.TargetSeconds), action.TargetSide, action.Frequency, action.Points, action.Contraindications, action.VideoURL, boolInt(action.IsLocked), action.ID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return userErr(ErrNotFound, "找不到这个动作")
	}
	return nil
}

// Lock 锁定或解锁。level 为 0 表示锁整个模块，否则锁某一级。
func (s *Store) Lock(userID, moduleID, level int, locked bool, reason string) error {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "康复师操作"
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	progress, err := s.progressOf(tx, userID, moduleID)
	if err != nil {
		return err
	}
	kind := "锁定"
	if !locked {
		kind = "解锁"
	}
	var actionID = 0
	from := progress.CurrentLevel
	if level <= 0 {
		res, err := tx.Exec(`UPDATE modules SET is_locked=? WHERE id=?`, boolInt(locked), moduleID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return userErr(ErrNotFound, "找不到这个模块")
		}
		if locked {
			if _, err := tx.Exec(`UPDATE user_module_progress SET status='已锁定', updated_at=? WHERE user_id=? AND module_id=? AND status!='已完成'`, now(), userID, moduleID); err != nil {
				return err
			}
		} else if _, err := tx.Exec(`UPDATE user_module_progress SET status='进行中', updated_at=? WHERE user_id=? AND module_id=? AND status='已锁定'`, now(), userID, moduleID); err != nil {
			return err
		}
		_ = tx.QueryRow(`SELECT id FROM actions WHERE module_id=? AND level=?`, moduleID, progress.CurrentLevel).Scan(&actionID)
	} else {
		res, err := tx.Exec(`UPDATE actions SET is_locked=? WHERE module_id=? AND level=?`, boolInt(locked), moduleID, level)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return userErr(ErrNotFound, "找不到这个等级")
		}
		if err := tx.QueryRow(`SELECT id FROM actions WHERE module_id=? AND level=?`, moduleID, level).Scan(&actionID); err != nil {
			return err
		}
		if level == progress.CurrentLevel && locked && progress.Status != "已完成" {
			if _, err := tx.Exec(`UPDATE user_module_progress SET status='已锁定', updated_at=? WHERE user_id=? AND module_id=?`, now(), userID, moduleID); err != nil {
				return err
			}
		}
		if level == progress.CurrentLevel && !locked && progress.Status == "已锁定" {
			var moduleLocked int
			if err := tx.QueryRow(`SELECT is_locked FROM modules WHERE id=?`, moduleID).Scan(&moduleLocked); err != nil {
				return err
			}
			if moduleLocked == 0 {
				if _, err := tx.Exec(`UPDATE user_module_progress SET status='进行中', updated_at=? WHERE user_id=? AND module_id=?`, now(), userID, moduleID); err != nil {
					return err
				}
			}
		}
	}
	if _, err := tx.Exec(`INSERT INTO upgrade_logs(user_id, module_id, action_id, from_level, to_level, type, reason, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, userID, moduleID, actionID, from, from, kind, reason, now()); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ActionsOf(moduleID int) ([]model.Action, error) {
	rows, err := s.DB.Query(`SELECT id, module_id, level, name, target_sets, target_reps, target_seconds, target_side, frequency, points, contraindications, COALESCE(video_url,''), is_locked FROM actions WHERE module_id=? ORDER BY level`, moduleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []model.Action{}
	for rows.Next() {
		action, err := scanAction(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, action)
	}
	if len(list) == 0 {
		return nil, userErr(ErrNotFound, "找不到这个模块的动作")
	}
	return list, rows.Err()
}

type activityInfo struct {
	last         map[int]string
	week         map[int]int
	trainedToday map[int]bool
	todayCount   int
}

func (s *Store) moduleCards(userID int) ([]model.ModuleCard, activityInfo, error) {
	info := activityInfo{last: map[int]string{}, week: map[int]int{}, trainedToday: map[int]bool{}}
	rows, err := s.DB.Query(`SELECT module_id, MAX(date), SUM(CASE WHEN date>=? THEN 1 ELSE 0 END) FROM workout_sessions WHERE user_id=? AND total_sets>0 AND status!='跳过' GROUP BY module_id`, weekStart(), userID)
	if err != nil {
		return nil, info, err
	}
	for rows.Next() {
		var moduleID int
		var week sql.NullInt64
		var last string
		if err := rows.Scan(&moduleID, &last, &week); err != nil {
			rows.Close()
			return nil, info, err
		}
		info.last[moduleID] = last
		info.week[moduleID] = int(week.Int64)
	}
	rows.Close()
	todayRows, err := s.DB.Query(`SELECT DISTINCT module_id FROM workout_sessions WHERE user_id=? AND date=? AND total_sets>0 AND status!='跳过'`, userID, today())
	if err != nil {
		return nil, info, err
	}
	for todayRows.Next() {
		var moduleID int
		if err := todayRows.Scan(&moduleID); err != nil {
			todayRows.Close()
			return nil, info, err
		}
		info.trainedToday[moduleID] = true
	}
	todayRows.Close()
	info.todayCount = len(info.trainedToday)

	cardRows, err := s.DB.Query(`SELECT m.id, m.name, m.purpose, m.sort_order, m.is_locked, p.current_level, p.max_level, p.status, a.id, a.name, a.is_locked, a.frequency
		FROM modules m
		JOIN user_module_progress p ON p.module_id=m.id AND p.user_id=?
		JOIN actions a ON a.module_id=m.id AND a.level=p.current_level
		ORDER BY m.sort_order`, userID)
	if err != nil {
		return nil, info, err
	}
	defer cardRows.Close()
	cards := []model.ModuleCard{}
	for cardRows.Next() {
		var card model.ModuleCard
		var moduleLocked, actionLocked int
		if err := cardRows.Scan(&card.ID, &card.Name, &card.Purpose, &card.SortOrder, &moduleLocked, &card.CurrentLevel, &card.MaxLevel, &card.Status, &card.CurrentActionID, &card.CurrentActionName, &actionLocked, &card.Frequency); err != nil {
			return nil, info, err
		}
		card.IsLocked = moduleLocked == 1
		card.CurrentActionLocked = actionLocked == 1
		rawStatus := card.Status
		if card.IsLocked || (card.CurrentActionLocked && rawStatus != "已完成") {
			card.Status = "已锁定"
		}
		card.ClearedLevels = card.CurrentLevel - 1
		if rawStatus == "已完成" {
			card.ClearedLevels = 10
		}
		if card.ClearedLevels < 0 {
			card.ClearedLevels = 0
		}
		card.DueToday = logic.DueToday(card.Frequency, info.last[card.ID], info.week[card.ID], today())
		cards = append(cards, card)
	}
	return cards, info, cardRows.Err()
}

func (s *Store) evaluate(q queryer, sessionID int) (model.FinishResult, error) {
	session, err := s.sessionByID(q, sessionID)
	if err != nil {
		return model.FinishResult{}, err
	}
	sets, err := s.setsOf(q, sessionID)
	if err != nil {
		return model.FinishResult{}, err
	}
	view, err := s.upgradeView(q, sessionID)
	if err != nil {
		return model.FinishResult{}, err
	}
	var previous int
	var previousFlag int
	err = q.QueryRow(`SELECT symptom_flag FROM workout_sessions WHERE user_id=? AND action_id=? AND id!=? AND status IN ('完成','中断') AND total_sets>0 ORDER BY id DESC LIMIT 1`, session.UserID, session.ActionID, sessionID).Scan(&previousFlag)
	flags := []bool{session.SymptomFlag}
	if err == nil {
		previous = 1
		flags = append(flags, previousFlag == 1)
	} else if !errors.Is(err, sql.ErrNoRows) {
		return model.FinishResult{}, err
	}
	_ = previous
	advice := logic.JudgeRollback(flags, session.Level)
	warning := ""
	for _, set := range sets {
		if text := logic.WarningText(set.PainScore, set.SymptomType); text != "" {
			warning = text
			break
		}
	}
	if sets == nil {
		sets = []model.SetLog{}
	}
	return model.FinishResult{Session: session, Upgrade: view, Rollback: model.RollbackView{Suggest: advice.Suggest, CanRollback: advice.CanRollback, Message: advice.Message}, Warning: warning, Sets: sets}, nil
}

func (s *Store) upgradeView(q queryer, sessionID int) (model.UpgradeView, error) {
	var reps, secs sql.NullInt64
	var sets, level int
	var side string
	err := q.QueryRow(`SELECT a.target_sets, a.target_reps, a.target_seconds, a.target_side, a.level FROM workout_sessions s JOIN actions a ON a.id=s.action_id WHERE s.id=?`, sessionID).Scan(&sets, &reps, &secs, &side, &level)
	if errors.Is(err, sql.ErrNoRows) {
		return model.UpgradeView{}, userErr(ErrNotFound, "找不到这次训练")
	}
	if err != nil {
		return model.UpgradeView{}, err
	}
	logs, err := s.setsOf(q, sessionID)
	if err != nil {
		return model.UpgradeView{}, err
	}
	records := make([]logic.SetRecord, 0, len(logs))
	for _, set := range logs {
		records = append(records, logic.SetRecord{SetNo: set.SetNo, Side: set.Side, Reps: set.Reps, Seconds: set.Seconds, PainScore: set.PainScore, SymptomType: set.SymptomType, FormOK: set.FormOK})
	}
	target := logic.Target{Sets: sets, Side: side}
	if reps.Valid {
		target.Reps = int(reps.Int64)
	}
	if secs.Valid {
		target.Seconds = int(secs.Int64)
	}
	decision := logic.JudgeUpgrade(records, target)
	view := model.UpgradeView{Eligible: decision.Eligible, GoodSets: decision.GoodSets, NeedSets: decision.NeedSets, Reasons: decision.Reasons, NextLevel: level}
	if decision.Eligible && level < 10 {
		view.NextLevel = level + 1
	}
	if decision.Eligible && level >= 10 {
		view.ModuleDone = true
		view.NextLevel = 10
	}
	if view.Reasons == nil {
		view.Reasons = []string{}
	}
	return view, nil
}

func (s *Store) symptomFlag(q queryer, sessionID int) (int, error) {
	sets, err := s.setsOf(q, sessionID)
	if err != nil {
		return 0, err
	}
	for _, set := range sets {
		if logic.WarningText(set.PainScore, set.SymptomType) != "" {
			return 1, nil
		}
	}
	return 0, nil
}

func (s *Store) activeSession(q queryer, userID int) (*model.Session, error) {
	var id int
	err := q.QueryRow(`SELECT id FROM workout_sessions WHERE user_id=? AND status='进行中' ORDER BY id DESC LIMIT 1`, userID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	session, err := s.sessionByID(q, id)
	if err != nil {
		return nil, err
	}
	return &session, nil
}

func (s *Store) sessionByID(q queryer, id int) (model.Session, error) {
	var session model.Session
	var flag int
	err := q.QueryRow(`SELECT s.id, s.user_id, s.module_id, s.action_id, a.name, m.name, a.level, s.date, s.status, s.total_sets, s.symptom_flag, COALESCE(s.note,''), s.created_at
		FROM workout_sessions s JOIN actions a ON a.id=s.action_id JOIN modules m ON m.id=s.module_id WHERE s.id=?`, id).Scan(
		&session.ID, &session.UserID, &session.ModuleID, &session.ActionID, &session.ActionName, &session.ModuleName, &session.Level, &session.Date, &session.Status, &session.TotalSets, &flag, &session.Note, &session.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return model.Session{}, userErr(ErrNotFound, "找不到这次训练")
	}
	if err != nil {
		return model.Session{}, err
	}
	session.SymptomFlag = flag == 1
	return session, nil
}

func (s *Store) setsOf(q queryer, sessionID int) ([]model.SetLog, error) {
	rows, err := q.Query(`SELECT id, session_id, set_no, reps, seconds, side, rpe, pain_score, symptom_type, form_ok, form_quality, created_at FROM workout_set_logs WHERE session_id=? ORDER BY set_no, id`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []model.SetLog{}
	for rows.Next() {
		var set model.SetLog
		var formOK int
		if err := rows.Scan(&set.ID, &set.SessionID, &set.SetNo, &set.Reps, &set.Seconds, &set.Side, &set.RPE, &set.PainScore, &set.SymptomType, &formOK, &set.FormQuality, &set.CreatedAt); err != nil {
			return nil, err
		}
		set.FormOK = formOK == 1
		list = append(list, set)
	}
	return list, rows.Err()
}

func (s *Store) actionByID(q queryer, id int) (model.Action, error) {
	return s.actionWithModule(q, id)
}

func (s *Store) actionWithModule(q queryer, id int) (model.Action, error) {
	var action model.Action
	var reps, secs sql.NullInt64
	var locked int
	var points, contra, video sql.NullString
	err := q.QueryRow(`SELECT a.id, a.module_id, a.level, a.name, a.target_sets, a.target_reps, a.target_seconds, a.target_side, a.frequency, a.points, a.contraindications, COALESCE(a.video_url,''), a.is_locked, m.name, m.purpose
		FROM actions a JOIN modules m ON m.id=a.module_id WHERE a.id=?`, id).Scan(
		&action.ID, &action.ModuleID, &action.Level, &action.Name, &action.TargetSets, &reps, &secs, &action.TargetSide, &action.Frequency, &points, &contra, &video, &locked, &action.ModuleName, &action.Purpose)
	if errors.Is(err, sql.ErrNoRows) {
		return model.Action{}, userErr(ErrNotFound, "找不到这个动作")
	}
	if err != nil {
		return model.Action{}, err
	}
	action.TargetReps = intPtr(reps)
	action.TargetSeconds = intPtr(secs)
	action.Points = points.String
	action.Contraindications = contra.String
	action.VideoURL = video.String
	action.IsLocked = locked == 1
	return action, nil
}

func (s *Store) progressOf(q queryer, userID, moduleID int) (model.Progress, error) {
	var progress model.Progress
	err := q.QueryRow(`SELECT p.user_id, p.module_id, m.name, p.current_level, p.max_level, p.status FROM user_module_progress p JOIN modules m ON m.id=p.module_id WHERE p.user_id=? AND p.module_id=?`, userID, moduleID).Scan(&progress.UserID, &progress.ModuleID, &progress.ModuleName, &progress.CurrentLevel, &progress.MaxLevel, &progress.Status)
	if errors.Is(err, sql.ErrNoRows) {
		return model.Progress{}, userErr(ErrNotFound, "找不到这个用户的模块进度")
	}
	return progress, err
}

func (s *Store) history(q queryer, where string, args []any, limit int) ([]model.Session, error) {
	query := `SELECT s.id, s.user_id, s.module_id, s.action_id, a.name, m.name, a.level, s.date, s.status, s.total_sets, s.symptom_flag, COALESCE(s.note,''), s.created_at
		FROM workout_sessions s JOIN actions a ON a.id=s.action_id JOIN modules m ON m.id=s.module_id ` + where + ` ORDER BY s.id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := q.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []model.Session{}
	for rows.Next() {
		var session model.Session
		var flag int
		if err := rows.Scan(&session.ID, &session.UserID, &session.ModuleID, &session.ActionID, &session.ActionName, &session.ModuleName, &session.Level, &session.Date, &session.Status, &session.TotalSets, &flag, &session.Note, &session.CreatedAt); err != nil {
			return nil, err
		}
		session.SymptomFlag = flag == 1
		list = append(list, session)
	}
	return list, rows.Err()
}

func (s *Store) ensureUser(id int) error {
	return s.ensureUserTx(s.DB, id)
}

func (s *Store) ensureUserTx(q queryer, id int) error {
	var n int
	if err := q.QueryRow(`SELECT COUNT(*) FROM users WHERE id=?`, id).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return userErr(ErrNotFound, "找不到这个用户")
	}
	return nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanAction(row scanner) (model.Action, error) {
	var action model.Action
	var reps, secs sql.NullInt64
	var locked int
	var points, contra, video sql.NullString
	err := row.Scan(&action.ID, &action.ModuleID, &action.Level, &action.Name, &action.TargetSets, &reps, &secs, &action.TargetSide, &action.Frequency, &points, &contra, &video, &locked)
	if err != nil {
		return model.Action{}, err
	}
	action.TargetReps = intPtr(reps)
	action.TargetSeconds = intPtr(secs)
	action.Points = points.String
	action.Contraindications = contra.String
	action.VideoURL = video.String
	action.IsLocked = locked == 1
	return action, nil
}

func actionState(level, current int, status string, moduleLocked, actionLocked bool) string {
	if moduleLocked || actionLocked {
		if status == "已完成" || level < current {
			return "done"
		}
		return "locked"
	}
	if status == "已完成" || level < current {
		return "done"
	}
	if level == current {
		return "current"
	}
	return "future"
}

func normalizeSet(in AddSetInput) (AddSetInput, error) {
	if in.SessionID <= 0 {
		return in, userErr(ErrBadInput, "缺少训练编号")
	}
	if in.SetNo <= 0 {
		return in, userErr(ErrBadInput, "组号要从 1 开始")
	}
	if in.Reps < 0 || in.Seconds < 0 {
		return in, userErr(ErrBadInput, "次数和秒数不能是负数")
	}
	if in.RPE < 1 || in.RPE > 10 {
		return in, userErr(ErrBadInput, "RPE 要在 1 到 10 之间。1 是很轻松，10 是完全力竭")
	}
	if in.PainScore < 0 || in.PainScore > 10 {
		return in, userErr(ErrBadInput, "疼痛评分要在 0 到 10 之间")
	}
	if in.SymptomType == "" {
		in.SymptomType = "无"
	}
	if !symptomTypes[in.SymptomType] {
		return in, userErr(ErrBadInput, "症状类型不在可选范围内")
	}
	if !formTypes[in.FormQuality] {
		return in, userErr(ErrBadInput, "动作质量请选择标准、代偿或无法完成")
	}
	if !sideTypes[in.Side] {
		return in, userErr(ErrBadInput, "左右侧请选择无、左、右或双侧")
	}
	return in, nil
}

func intPtr(v sql.NullInt64) *int {
	if !v.Valid {
		return nil
	}
	n := int(v.Int64)
	return &n
}

func nullInt(v *int) sql.NullInt64 {
	if v == nil || *v <= 0 {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: int64(*v), Valid: true}
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func today() string { return time.Now().Format("2006-01-02") }

func now() string { return time.Now().Format(time.RFC3339) }

func weekStart() string {
	current := time.Now()
	weekday := int(current.Weekday())
	if weekday == 0 {
		weekday = 7
	}
	return current.AddDate(0, 0, -(weekday - 1)).Format("2006-01-02")
}

func trimRunes(text string, max int) string {
	runes := []rune(text)
	if len(runes) <= max {
		return text
	}
	return string(runes[:max])
}
