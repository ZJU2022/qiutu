package store

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"qiutu/internal/db"
	"qiutu/internal/logic"
)

func TestSeedAndTrainingFlow(t *testing.T) {
	database := openTestDB(t)
	st := New(database)

	var modules, actions int
	if err := database.QueryRow(`SELECT COUNT(*) FROM modules`).Scan(&modules); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM actions`).Scan(&actions); err != nil {
		t.Fatal(err)
	}
	if modules != 11 || actions != 61 {
		t.Fatalf("种子数据应该是 11 个模块、61 个动作，得到 %d 个模块、%d 个动作", modules, actions)
	}
	var pushCount int
	if err := database.QueryRow(`SELECT COUNT(*) FROM actions WHERE module_id = (SELECT module_id FROM actions WHERE name = '墙壁俯卧撑')`).Scan(&pushCount); err != nil {
		t.Fatal(err)
	}
	if pushCount != 10 {
		t.Fatalf("俯卧撑阶梯应该仍是 10 级，得到 %d", pushCount)
	}

	home, err := st.Home(1)
	if err != nil {
		t.Fatal(err)
	}
	if len(home.Modules) != 11 {
		t.Fatalf("首页应该有 11 个模块，得到 %d", len(home.Modules))
	}
	if len(home.Suggestions) == 0 || len(home.Suggestions) > logic.DailyModuleLimit {
		t.Fatalf("今天的建议应该是 1 到 %d 个，得到 %d", logic.DailyModuleLimit, len(home.Suggestions))
	}
	setLevel, err := st.SetLevel(1, 3, 4)
	if err != nil || setLevel.CurrentLevel != 4 {
		t.Fatalf("应该能自己把死虫设到第 4 级，得到 %+v %v", setLevel, err)
	}
	if _, err := st.SetLevel(1, 3, 99); err == nil {
		t.Fatal("超出这个项目的等级时应该失败")
	}

	var loadedID int
	if err := database.QueryRow(`SELECT id FROM actions WHERE name = '【进阶】负重臀桥'`).Scan(&loadedID); err != nil {
		t.Fatal(err)
	}
	loaded, err := st.ActionDetail(1, loadedID)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.Action.IsLocked || loaded.CanTrain {
		t.Fatalf("负重臀桥应该默认锁定，得到 locked=%v canTrain=%v", loaded.Action.IsLocked, loaded.CanTrain)
	}

	var pushModule int
	if err := database.QueryRow(`SELECT module_id FROM actions WHERE name = '墙壁俯卧撑'`).Scan(&pushModule); err != nil {
		t.Fatal(err)
	}
	detail, err := st.ModuleDetail(1, pushModule)
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Actions) != 10 || detail.Actions[0].Name != "墙壁俯卧撑" || detail.Actions[0].State != "current" {
		t.Fatalf("胸肌模块阶梯不对: %+v", detail.Actions[0])
	}

	session, err := st.CreateSession(1, pushModule, detail.Actions[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	for setNo := 1; setNo <= 3; setNo++ {
		if _, err := st.AddSet(AddSetInput{SessionID: session.ID, SetNo: setNo, Reps: 8, Side: "无", RPE: 5, PainScore: 0, SymptomType: "无", FormQuality: "标准"}); err != nil {
			t.Fatal(err)
		}
	}
	finished, err := st.Finish(session.ID, "完成", "")
	if err != nil {
		t.Fatal(err)
	}
	if !finished.Upgrade.Eligible || finished.Upgrade.NextLevel != 2 {
		t.Fatalf("3 组达标后应该可以升到 2 级，得到 %+v", finished.Upgrade)
	}
	progress, err := st.ConfirmUpgrade(session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if progress.CurrentLevel != 2 {
		t.Fatalf("确认后应该是 2 级，得到 %d", progress.CurrentLevel)
	}
	again, err := st.ConfirmUpgrade(session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !again.Already || again.CurrentLevel != 2 {
		t.Fatalf("同一次训练不能重复升级，得到 %+v", again)
	}

	dangerSession, err := st.CreateSession(1, pushModule, detail.Actions[1].ID)
	if err != nil {
		t.Fatal(err)
	}
	warned, err := st.AddSet(AddSetInput{SessionID: dangerSession.ID, SetNo: 1, Reps: 8, Side: "无", RPE: 8, PainScore: 0, SymptomType: "腿麻", FormQuality: "标准"})
	if err != nil {
		t.Fatal(err)
	}
	if warned.Warning == "" {
		t.Fatal("腿麻应该弹出停止警告")
	}
	dangerFinish, err := st.Finish(dangerSession.ID, "中断", "腿麻")
	if err != nil {
		t.Fatal(err)
	}
	if dangerFinish.Upgrade.Eligible || !dangerFinish.Rollback.Suggest {
		t.Fatalf("有危险症状时不能升级，并且要建议回退，得到 upgrade=%+v rollback=%+v", dangerFinish.Upgrade, dangerFinish.Rollback)
	}
	rolled, err := st.Rollback(1, pushModule, "腿麻，先巩固上一级", dangerSession.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rolled.CurrentLevel != 1 {
		t.Fatalf("回退后应该回到 1 级，得到 %d", rolled.CurrentLevel)
	}

	moduleRows, err := database.Query(`SELECT id FROM modules WHERE id != ? ORDER BY sort_order`, pushModule)
	if err != nil {
		t.Fatal(err)
	}
	var others []int
	for moduleRows.Next() {
		var id int
		if err := moduleRows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		others = append(others, id)
	}
	moduleRows.Close()
	// 俯卧撑这次测试里已经占掉 1 个名额。
	used := 1
	blocked := false
	for _, moduleID := range others {
		var actionID int
		if err := database.QueryRow(`SELECT a.id FROM actions a JOIN user_module_progress p ON p.module_id = a.module_id AND p.user_id = 1 AND a.level = p.current_level WHERE a.module_id = ?`, moduleID).Scan(&actionID); err != nil {
			t.Fatal(err)
		}
		sess, err := st.CreateSession(1, moduleID, actionID)
		if used >= logic.DailyModuleLimit {
			if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("%d 个小训练", logic.DailyModuleLimit)) {
				t.Fatalf("超过每天 %d 个应该被拦住，得到 %v", logic.DailyModuleLimit, err)
			}
			blocked = true
			break
		}
		if err != nil {
			t.Fatalf("开始模块 %d 失败: %v", moduleID, err)
		}
		if _, err := st.AddSet(AddSetInput{SessionID: sess.ID, SetNo: 1, Reps: 8, Side: "无", RPE: 5, SymptomType: "无", FormQuality: "标准"}); err != nil {
			t.Fatal(err)
		}
		if _, err := st.Finish(sess.ID, "完成", ""); err != nil {
			t.Fatal(err)
		}
		used++
	}
	if !blocked {
		t.Fatal("每日上限没有生效")
	}

	if err := st.Lock(1, others[len(others)-1], 1, true, "测试锁定"); err != nil {
		t.Fatal(err)
	}
	var lockedID int
	if err := database.QueryRow(`SELECT id FROM actions WHERE module_id = ? AND level = 1`, others[len(others)-1]).Scan(&lockedID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateSession(1, others[len(others)-1], lockedID); err == nil || !strings.Contains(err.Error(), "锁定") {
		t.Fatalf("锁定后不能训练，得到 %v", err)
	}
}

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	database, err := db.Open(filepath.Join(t.TempDir(), "qiutu.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database
}
