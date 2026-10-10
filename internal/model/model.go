// Package model 存放数据的“形状”。
// 可以把它想成表格的表头：有哪些字段、发给页面时叫什么名字。
// 这里不做判断，判断放在 logic，存取放在 store。
package model

// ModuleCard 是首页和模块列表上的一张卡片。
type ModuleCard struct {
	ID                  int    `json:"id"`
	Name                string `json:"name"`
	Purpose             string `json:"purpose"`
	SortOrder           int    `json:"sort_order"`
	IsLocked            bool   `json:"is_locked"`
	CurrentLevel        int    `json:"current_level"`
	MaxLevel            int    `json:"max_level"`
	Status              string `json:"status"`
	CurrentActionID     int    `json:"current_action_id"`
	CurrentActionName   string `json:"current_action_name"`
	CurrentActionLocked bool   `json:"current_action_locked"`
	Frequency           string `json:"frequency"`
	DueToday            bool   `json:"due_today"`
	ClearedLevels       int    `json:"cleared_levels"`
}

// Action 是某一个等级上的一个动作。
// 指针字段（*int）表示“可以没有”：比如有的动作记次数，有的只记秒数。
type Action struct {
	ID                int    `json:"id"`
	ModuleID          int    `json:"module_id"`
	ModuleName        string `json:"module_name,omitempty"`
	Purpose           string `json:"purpose,omitempty"`
	Level             int    `json:"level"`
	Name              string `json:"name"`
	TargetSets        int    `json:"target_sets"`
	TargetReps        *int   `json:"target_reps"`
	TargetSeconds     *int   `json:"target_seconds"`
	TargetSide        string `json:"target_side"`
	Frequency         string `json:"frequency"`
	Points            string `json:"points"`
	Contraindications string `json:"contraindications"`
	VideoURL          string `json:"video_url"`
	IsLocked          bool   `json:"is_locked"`
	// State 给页面上色用：current 当前级，done 已通过，future 还没解锁，locked 被锁住。
	State string `json:"state,omitempty"`
}

// Session 是一次训练。一次训练里面会有好几组。
type Session struct {
	ID          int    `json:"id"`
	UserID      int    `json:"user_id"`
	ModuleID    int    `json:"module_id"`
	ActionID    int    `json:"action_id"`
	ActionName  string `json:"action_name,omitempty"`
	ModuleName  string `json:"module_name,omitempty"`
	Level       int    `json:"level,omitempty"`
	Date        string `json:"date"`
	Status      string `json:"status"`
	TotalSets   int    `json:"total_sets"`
	SymptomFlag bool   `json:"symptom_flag"`
	Note        string `json:"note"`
	CreatedAt   string `json:"created_at"`
}

// SetLog 是一组训练的记录。
type SetLog struct {
	ID          int    `json:"id"`
	SessionID   int    `json:"session_id"`
	SetNo       int    `json:"set_no"`
	Reps        int    `json:"reps"`
	Seconds     int    `json:"seconds"`
	Side        string `json:"side"`
	RPE         int    `json:"rpe"`
	PainScore   int    `json:"pain_score"`
	SymptomType string `json:"symptom_type"`
	FormOK      bool   `json:"form_ok"`
	FormQuality string `json:"form_quality"`
	CreatedAt   string `json:"created_at"`
}

// Symptom 是单独记下来的一次不舒服。
type Symptom struct {
	ID          int    `json:"id"`
	UserID      int    `json:"user_id"`
	Date        string `json:"date"`
	BodyPart    string `json:"body_part"`
	SymptomType string `json:"symptom_type"`
	Severity    int    `json:"severity"`
	Note        string `json:"note"`
	CreatedAt   string `json:"created_at"`
}

// UpgradeLog 记录升级、回退、锁定、解锁。方便以后回看“为什么变了”。
type UpgradeLog struct {
	ID         int    `json:"id"`
	UserID     int    `json:"user_id"`
	ModuleID   int    `json:"module_id"`
	ModuleName string `json:"module_name,omitempty"`
	ActionID   int    `json:"action_id"`
	FromLevel  int    `json:"from_level"`
	ToLevel    int    `json:"to_level"`
	Type       string `json:"type"`
	Reason     string `json:"reason"`
	CreatedAt  string `json:"created_at"`
}

// Progress 是某个用户在某个模块上练到了哪一级。
type Progress struct {
	UserID       int    `json:"user_id"`
	ModuleID     int    `json:"module_id"`
	ModuleName   string `json:"module_name,omitempty"`
	CurrentLevel int    `json:"current_level"`
	MaxLevel     int    `json:"max_level"`
	Status       string `json:"status"`
	Already      bool   `json:"already,omitempty"`
}

// User 是使用系统的人。MVP 先内置一个训练者和一个康复师演示账号。
type User struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Role string `json:"role"`
}

// Home 是首页一次拿齐的数据。
type Home struct {
	SafetyTip   string       `json:"safety_tip"`
	Disclaimer  string       `json:"disclaimer"`
	DailyLimit  int          `json:"daily_limit"`
	TodayCount  int          `json:"today_count"`
	RestToday   bool         `json:"rest_today"`
	RestMessage string       `json:"rest_message,omitempty"`
	Suggestions []Suggestion `json:"suggestions"`
	Modules     []ModuleCard `json:"modules"`
	Active      *Session     `json:"active,omitempty"`
}

// Suggestion 是今天建议练的一个动作。
type Suggestion struct {
	ModuleID   int    `json:"module_id"`
	ModuleName string `json:"module_name"`
	ActionID   int    `json:"action_id"`
	ActionName string `json:"action_name"`
	Level      int    `json:"level"`
	Reason     string `json:"reason"`
}

// ModuleDetail 是一个模块的 10 级阶梯。
type ModuleDetail struct {
	Module  ModuleCard `json:"module"`
	Actions []Action   `json:"actions"`
}

// ActionDetail 是动作详情，附带这个用户最近几次练它的记录。
type ActionDetail struct {
	Action   Action    `json:"action"`
	Progress Progress  `json:"progress"`
	History  []Session `json:"history"`
	CanTrain bool      `json:"can_train"`
	LockHint string    `json:"lock_hint,omitempty"`
}

// UpgradeView 是“这次能不能升级”的结论。
type UpgradeView struct {
	Eligible   bool     `json:"eligible"`
	GoodSets   int      `json:"good_sets"`
	NeedSets   int      `json:"need_sets"`
	NextLevel  int      `json:"next_level"`
	ModuleDone bool     `json:"module_done"`
	Reasons    []string `json:"reasons"`
}

// RollbackView 是“要不要建议回退”的结论。
type RollbackView struct {
	Suggest     bool   `json:"suggest"`
	CanRollback bool   `json:"can_rollback"`
	Message     string `json:"message,omitempty"`
}

// FinishResult 是结束一次训练后给完成页看的总结。
type FinishResult struct {
	Session  Session      `json:"session"`
	Sets     []SetLog     `json:"sets"`
	Upgrade  UpgradeView  `json:"upgrade"`
	Rollback RollbackView `json:"rollback"`
	Warning  string       `json:"warning,omitempty"`
}

// SetResult 是记下某一组之后的回应。有危险症状时 Warning 不为空。
type SetResult struct {
	Set     SetLog `json:"set"`
	Warning string `json:"warning,omitempty"`
}

// DayStat 是某一天练了几次，用来画日历。
type DayStat struct {
	Date     string `json:"date"`
	Sessions int    `json:"sessions"`
}

// Stats 是进度页用的汇总数字。
type Stats struct {
	TotalSessions int          `json:"total_sessions"`
	TotalSets     int          `json:"total_sets"`
	TotalReps     int          `json:"total_reps"`
	TotalSeconds  int          `json:"total_seconds"`
	Days          []DayStat    `json:"days"`
	Symptoms      []Symptom    `json:"symptoms"`
	Events        []UpgradeLog `json:"events"`
	Modules       []ModuleCard `json:"modules"`
}
