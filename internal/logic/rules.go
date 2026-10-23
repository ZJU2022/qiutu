// Package logic 只负责“规则”，不碰数据库，也不碰网页。
// 这样做的好处是：规则可以单独考试（写测试），改规则时不用启动整个网站。
package logic

import (
	"fmt"
	"time"
)

// PainStopThreshold 疼痛达到这个分数就要停。0 是完全不痛，10 是极痛。
const PainStopThreshold = 3

// DailyModuleLimit 一天最多练几个小训练。康复师的安排是每天选 3 到 5 个，这里卡住上限。
const DailyModuleLimit = 5

// dangerSymptoms 是必须马上停止的症状。
// “酸胀”不在里面：练完有点酸，常常是正常的用力感觉。
var dangerSymptoms = map[string]bool{
	"腰疼":  true,
	"腿麻":  true,
	"电流感": true,
	"刺痛":  true,
}

// SetRecord 是规则眼里的“一组”。字段只保留做判断需要的东西。
type SetRecord struct {
	SetNo       int
	Side        string
	Reps        int
	Seconds     int
	PainScore   int
	SymptomType string
	FormOK      bool
}

// Target 是这个动作的升级目标，来自动作表，不是写死在判断里的。
type Target struct {
	Sets    int
	Reps    int // 0 表示不要求次数
	Seconds int // 0 表示不要求秒数
	Side    string
}

// Decision 是升级判断的结果。Reasons 用来告诉用户“为什么还不能升”。
type Decision struct {
	Eligible bool
	GoodSets int
	NeedSets int
	Reasons  []string
}

// RollbackAdvice 告诉完成页要不要出现“建议回退”。
type RollbackAdvice struct {
	Suggest     bool
	CanRollback bool
	Message     string
}

// IsDanger 判断这一组是不是触发了安全红线。
func IsDanger(pain int, symptom string) bool {
	if pain >= PainStopThreshold {
		return true
	}
	return dangerSymptoms[symptom]
}

// WarningText 返回要弹给用户看的那句话。没有危险时返回空字符串。
func WarningText(pain int, symptom string) string {
	if IsDanger(pain, symptom) {
		return "建议立即停止，记录症状，并联系康复师。"
	}
	if symptom == "其他" {
		return "你记录了其他症状。建议先停止，把不舒服写清楚，并联系康复师。这次训练不能升级。"
	}
	return ""
}

// JudgeUpgrade 看这一次训练的全部组，决定能不能升级。
// 规则对应产品文档第 7.1 节：组数够、每组达标、动作标准、没有危险症状。
func JudgeUpgrade(sets []SetRecord, target Target) Decision {
	need := target.Sets
	if need <= 0 {
		need = 3
	}
	reasons := make([]string, 0)
	if len(sets) == 0 {
		reasons = append(reasons, "这次还没有记下任何一组")
		return Decision{NeedSets: need, Reasons: reasons}
	}

	for _, set := range sets {
		if IsDanger(set.PainScore, set.SymptomType) {
			reasons = append(reasons, "本次出现需要停止的症状，不能升级")
			break
		}
		if set.SymptomType == "其他" {
			reasons = append(reasons, "本次有还没说清楚的其他症状，不能升级")
			break
		}
	}

	good := countGoodSets(sets, target)
	if good < need {
		reasons = append(reasons, fmt.Sprintf("达标 %d 组，目标是 %d 组", good, need))
	}
	return Decision{
		Eligible: len(reasons) == 0,
		GoodSets: good,
		NeedSets: need,
		Reasons:  reasons,
	}
}

// countGoodSets 按“第几组”来数。
// 双侧动作要左右都达标，才算完成这一组。
func countGoodSets(sets []SetRecord, target Target) int {
	grouped := map[int][]SetRecord{}
	order := make([]int, 0)
	for _, set := range sets {
		if _, seen := grouped[set.SetNo]; !seen {
			order = append(order, set.SetNo)
		}
		grouped[set.SetNo] = append(grouped[set.SetNo], set)
	}
	good := 0
	for _, setNo := range order {
		if groupOK(grouped[setNo], target) {
			good++
		}
	}
	return good
}

func groupOK(group []SetRecord, target Target) bool {
	switch target.Side {
	case "左", "右":
		return sideOK(group, target.Side, target)
	case "双侧":
		// 允许两种记法：左右各一条，或者一条直接写成“双侧”。
		if sideOK(group, "双侧", target) {
			return true
		}
		return sideOK(group, "左", target) && sideOK(group, "右", target)
	default:
		for _, set := range group {
			if recordOK(set, target) {
				return true
			}
		}
		return false
	}
}

func sideOK(group []SetRecord, side string, target Target) bool {
	for _, set := range group {
		if set.Side == side && recordOK(set, target) {
			return true
		}
	}
	return false
}

// recordOK 一组要同时满足：动作标准、没有危险、次数或时间达标。
func recordOK(set SetRecord, target Target) bool {
	if !set.FormOK || IsDanger(set.PainScore, set.SymptomType) || set.SymptomType == "其他" {
		return false
	}
	if target.Reps > 0 && set.Reps < target.Reps {
		return false
	}
	if target.Seconds > 0 && set.Seconds < target.Seconds {
		return false
	}
	return true
}

// JudgeRollback 根据最近几次有没有症状，决定要不要建议回退。
// newestFirst 的第 0 个是最新这次。level 是当前等级，1 级不能再降。
func JudgeRollback(newestFirst []bool, level int) RollbackAdvice {
	currentBad := len(newestFirst) > 0 && newestFirst[0]
	twoBad := len(newestFirst) >= 2 && newestFirst[0] && newestFirst[1]
	if !currentBad && !twoBad {
		return RollbackAdvice{}
	}

	message := "这次训练出现了需要停止的症状，建议回退或暂停，并联系康复师。"
	if twoBad {
		message = "同一个动作连续 2 次训练都出现症状，建议回退一级，并联系康复师。"
	}
	if level <= 1 {
		return RollbackAdvice{
			Suggest:     true,
			CanRollback: false,
			Message:     message + "你已经在第 1 级，不能再降低，请先暂停。",
		}
	}
	return RollbackAdvice{Suggest: true, CanRollback: true, Message: message}
}

// DueToday 按动作的频率，判断今天适不适合把它放进建议里。
// 这只影响“建议”，不会禁止用户打开动作。last 为空表示从来没练过。
func DueToday(freq, last string, sessionsThisWeek int, today string) bool {
	switch freq {
	case "每周2-3次":
		return sessionsThisWeek < 3
	case "隔天":
		if last == "" {
			return true
		}
		days, ok := daysBetween(last, today)
		if !ok {
			return true
		}
		return days >= 2
	default:
		// 每天、仅康复中心、自定义：都可以出现在建议里。
		return true
	}
}

func daysBetween(last, today string) (int, bool) {
	from, err1 := time.Parse("2006-01-02", last)
	to, err2 := time.Parse("2006-01-02", today)
	if err1 != nil || err2 != nil {
		return 0, false
	}
	return int(to.Sub(from).Hours() / 24), true
}
