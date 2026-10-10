package logic

import "testing"

func goodSet(no, reps int) SetRecord {
	return SetRecord{SetNo: no, Side: "无", Reps: reps, SymptomType: "无", FormOK: true}
}

func TestJudgeUpgradeThreeGoodSets(t *testing.T) {
	target := Target{Sets: 3, Reps: 8, Side: "无"}
	sets := []SetRecord{goodSet(1, 8), goodSet(2, 8), goodSet(3, 10)}
	got := JudgeUpgrade(sets, target)
	if !got.Eligible || got.GoodSets != 3 {
		t.Fatalf("3 组达标应该可以升级，得到 %+v", got)
	}
}

func TestJudgeUpgradeNotEnoughReps(t *testing.T) {
	target := Target{Sets: 3, Reps: 8, Side: "无"}
	sets := []SetRecord{goodSet(1, 8), goodSet(2, 7), goodSet(3, 8)}
	got := JudgeUpgrade(sets, target)
	if got.Eligible || got.GoodSets != 2 {
		t.Fatalf("有一组次数不够，不应该升级，得到 %+v", got)
	}
}

func TestJudgeUpgradePainBlocks(t *testing.T) {
	target := Target{Sets: 3, Reps: 8, Side: "无"}
	painful := goodSet(2, 8)
	painful.PainScore = 3
	sets := []SetRecord{goodSet(1, 8), painful, goodSet(3, 8)}
	got := JudgeUpgrade(sets, target)
	if got.Eligible {
		t.Fatalf("疼痛达到 3 分不能升级")
	}
}

func TestJudgeUpgradeSorenessCanPass(t *testing.T) {
	target := Target{Sets: 3, Reps: 8, Side: "无"}
	sore := goodSet(1, 8)
	sore.SymptomType = "酸胀"
	sore.PainScore = 2
	got := JudgeUpgrade([]SetRecord{sore, goodSet(2, 8), goodSet(3, 8)}, target)
	if !got.Eligible {
		t.Fatalf("酸胀且疼痛不到 3 分，动作又标准，应该可以升级，得到 %+v", got)
	}
}

func TestJudgeUpgradeSecondsAndBothSides(t *testing.T) {
	target := Target{Sets: 1, Seconds: 30, Side: "双侧"}
	sets := []SetRecord{
		{SetNo: 1, Side: "左", Seconds: 30, SymptomType: "无", FormOK: true},
		{SetNo: 1, Side: "右", Seconds: 29, SymptomType: "无", FormOK: true},
	}
	got := JudgeUpgrade(sets, target)
	if got.Eligible {
		t.Fatalf("右侧时间不够，双侧这一组不算完成")
	}
	sets[1].Seconds = 30
	got = JudgeUpgrade(sets, target)
	if !got.Eligible {
		t.Fatalf("左右都够 30 秒，应该可以升级，得到 %+v", got)
	}
}

func TestJudgeUpgradeNoRepTarget(t *testing.T) {
	target := Target{Sets: 2, Side: "无"}
	sets := []SetRecord{goodSet(1, 0), goodSet(2, 0)}
	got := JudgeUpgrade(sets, target)
	if !got.Eligible {
		t.Fatalf("没有次数和时间要求时，动作标准就算达标，得到 %+v", got)
	}
}

func TestWarningText(t *testing.T) {
	if WarningText(3, "无") == "" {
		t.Fatal("疼痛 3 分应该有警告")
	}
	if WarningText(0, "腿麻") == "" || WarningText(0, "电流感") == "" || WarningText(0, "刺痛") == "" {
		t.Fatal("腿麻、电流感、刺痛都应该有警告")
	}
	if WarningText(1, "酸胀") != "" {
		t.Fatal("单纯酸胀不应该弹出停止警告")
	}
}

func TestJudgeRollback(t *testing.T) {
	first := JudgeRollback([]bool{true}, 1)
	if !first.Suggest || first.CanRollback {
		t.Fatalf("1 级出现症状只能暂停，不能再降级，得到 %+v", first)
	}
	two := JudgeRollback([]bool{true, true}, 4)
	if !two.Suggest || !two.CanRollback {
		t.Fatalf("连续两次有症状，应该建议回退，得到 %+v", two)
	}
	ok := JudgeRollback([]bool{false, true}, 4)
	if ok.Suggest {
		t.Fatalf("这次没有症状，不应该因为更早的一次就建议回退")
	}
}

func TestDueToday(t *testing.T) {
	if DueToday("隔天", "2026-10-09", 0, "2026-10-10") {
		t.Fatal("昨天刚练过，隔天的动作今天不该建议")
	}
	if !DueToday("隔天", "2026-10-08", 0, "2026-10-10") {
		t.Fatal("前天练过，隔天的动作今天可以建议")
	}
	if DueToday("每周2-3次", "", 3, "2026-10-10") {
		t.Fatal("这周已经练了 3 次，不该再建议")
	}
	if !DueToday("每周2-3次", "", 2, "2026-10-10") {
		t.Fatal("这周练了 2 次，还可以建议")
	}
}
