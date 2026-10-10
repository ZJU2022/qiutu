package store

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"qiutu/internal/db"
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
	if modules != 9 || actions != 90 {
		t.Fatalf("种子数据应该是 9 个模块、90 个动作，得到 %d 个模块、%d 个动作", modules, actions)
	}
	rows, err := database.Query(`SELECT COUNT(*) FROM actions GROUP BY module_id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var n int
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 10 {
			t.Fatalf("每个模块都要有 10 个动作，得到 %d", n)
		}
	}

	home, err := st.Home(1)
	if err != nil {
		t.Fatal(err)
	}
	if len(home.Modules) != 9 {
		t.Fatalf("首页应该有 9 个模块，得到 %d", len(home.Modules))
	}
	if len(home.Suggestions) == 0 || len(home.Suggestions) > 3 {
		t.Fatalf("今天的建议应该是 1 到 3 个，得到 %d", len(home.Suggestions))
	}

	var handstandID int
	if err := database.QueryRow(`SELECT id FROM actions WHERE name = '标准倒立撑'`).Scan(&handstandID); err != nil {
		t.Fatal(err)
	}
	handstand, err := st.ActionDetail(1, handstandID)
	if err != nil {
		t.Fatal(err)
	}
	if !handstand.Action.IsLocked || handstand.CanTrain {
		t.Fatalf("标准倒立撑应该默认锁定，得到 locked=%v canTrain=%v", handstand.Action.IsLocked, handstand.CanTrain)
	}

	detail, err := st.ModuleDetail(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Actions) != 10 || detail.Actions[0].Name != "墙壁俯卧撑" || detail.Actions[0].State != "current" {
		t.Fatalf("胸肌模块阶梯不对: %+v", detail.Actions[0])
	}

	session, err := st.CreateSession(1, 1, detail.Actions[0].ID)
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

	dangerSession, err := st.CreateSession(1, 1, detail.Actions[1].ID)
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
	rolled, err := st.Rollback(1, 1, "腿麻，先巩固上一级", dangerSession.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rolled.CurrentLevel != 1 {
		t.Fatalf("回退后应该回到 1 级，得到 %d", rolled.CurrentLevel)
	}

	for moduleID := 1; moduleID <= 3; moduleID++ {
		var actionID int
		if err := database.QueryRow(`SELECT a.id FROM actions a JOIN user_module_progress p ON p.module_id = a.module_id AND p.user_id = 1 AND a.level = p.current_level WHERE a.module_id = ?`, moduleID).Scan(&actionID); err != nil {
			t.Fatal(err)
		}
		sess, err := st.CreateSession(1, moduleID, actionID)
		if err != nil {
			t.Fatalf("开始模块 %d 失败: %v", moduleID, err)
		}
		if _, err := st.AddSet(AddSetInput{SessionID: sess.ID, SetNo: 1, Reps: 8, Side: "无", RPE: 5, SymptomType: "无", FormQuality: "标准"}); err != nil {
			t.Fatal(err)
		}
		if _, err := st.Finish(sess.ID, "完成", ""); err != nil {
			t.Fatal(err)
		}
	}
	var fourth int
	if err := database.QueryRow(`SELECT id FROM actions WHERE module_id = 4 AND level = 1`).Scan(&fourth); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateSession(1, 4, fourth); err == nil || !strings.Contains(err.Error(), "3 个模块") {
		t.Fatalf("第 4 个模块应该被每日上限拦住，得到 %v", err)
	}

	if err := st.Lock(1, 5, 1, true, "测试锁定"); err != nil {
		t.Fatal(err)
	}
	var lockedID int
	if err := database.QueryRow(`SELECT id FROM actions WHERE module_id = 5 AND level = 1`).Scan(&lockedID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateSession(1, 5, lockedID); err == nil || !strings.Contains(err.Error(), "锁定") {
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
