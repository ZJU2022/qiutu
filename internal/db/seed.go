package db

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// act 是灌进数据库之前的一张动作草稿。
// Reps 或 Secs 用 -1 表示“这个动作不记这项”。两个都是 0 时，默认每组 8 次。
type act struct {
	Name  string
	Sets  int
	Reps  int
	Secs  int
	Side  string
	Freq  string
	Lock  bool
	Extra string
}

type mod struct {
	Name    string
	Purpose string
	Cue     string
	Actions []act
}

const baseContra = "出现腰疼、腿麻、电流感、刺痛，或疼痛评分达到 3 分及以上（0 分完全不痛，10 分痛到极点），立即停止。这里的记录不能代替康复师的诊断。"

func seed(database *sql.DB) error {
	var n int
	if err := database.QueryRow(`SELECT COUNT(*) FROM modules`).Scan(&n); err != nil {
		return fmt.Errorf("检查种子数据失败: %w", err)
	}
	if n > 0 {
		return nil
	}

	now := time.Now().Format(time.RFC3339)
	tx, err := database.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(`INSERT INTO users(id, name, role, created_at) VALUES (1, '训练者', 'user', ?), (2, '康复师', 'therapist', ?)`, now, now); err != nil {
		return fmt.Errorf("写入用户失败: %w", err)
	}

	for i, module := range catalog() {
		moduleID := i + 1
		if _, err := tx.Exec(
			`INSERT INTO modules(id, name, purpose, sort_order, is_locked, created_at) VALUES (?, ?, ?, ?, 0, ?)`,
			moduleID, module.Name, module.Purpose, moduleID, now,
		); err != nil {
			return fmt.Errorf("写入模块失败: %w", err)
		}
		if _, err := tx.Exec(
			`INSERT INTO user_module_progress(user_id, module_id, current_level, max_level, status, updated_at) VALUES (1, ?, 1, 1, '进行中', ?)`,
			moduleID, now,
		); err != nil {
			return fmt.Errorf("写入进度失败: %w", err)
		}
		for level, action := range module.Actions {
			reps, secs := actionTargets(action)
			side := action.Side
			if side == "" {
				side = "无"
			}
			freq := action.Freq
			if freq == "" {
				freq = "隔天"
			}
			sets := action.Sets
			if sets == 0 {
				sets = 3
			}
			if _, err := tx.Exec(`INSERT INTO actions(
				module_id, level, name, target_sets, target_reps, target_seconds, target_side,
				frequency, points, contraindications, video_url, sort_order, is_locked
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, '', ?, ?)`,
				moduleID, level+1, action.Name, sets, reps, secs, side, freq,
				buildPoints(module.Cue, action.Name, action.Extra),
				contra(action.Lock, action.Extra),
				level+1, boolInt(action.Lock),
			); err != nil {
				return fmt.Errorf("写入动作 %s 失败: %w", action.Name, err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return nil
}

func actionTargets(a act) (reps, secs sql.NullInt64) {
	if a.Reps > 0 {
		reps = sql.NullInt64{Int64: int64(a.Reps), Valid: true}
	} else if a.Reps == 0 && a.Secs <= 0 {
		reps = sql.NullInt64{Int64: 8, Valid: true}
	}
	if a.Secs > 0 {
		secs = sql.NullInt64{Int64: int64(a.Secs), Valid: true}
	}
	return reps, secs
}

func buildPoints(cue, name, extra string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "练「%s」时记住：\n%s\n", name, cue)
	b.WriteString("呼吸保持顺畅，不要憋气。\n")
	b.WriteString("如果身体用别的部位偷偷使劲，动作就不算标准。这种借力叫做代偿。\n")
	if extra != "" {
		b.WriteString(extra)
		if !strings.HasSuffix(extra, "\n") {
			b.WriteString("\n")
		}
	}
	b.WriteString("出现腰疼、腿麻、电流感、刺痛，马上停止。")
	return b.String()
}

func contra(lock bool, extra string) string {
	text := baseContra
	if extra != "" {
		text += "\n" + extra
	}
	if lock {
		text += "\n此动作默认锁定，康复师解锁后才能开始训练。"
	}
	return text
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func catalog() []mod {
	return []mod{
		{
			Name:    "胸肌训练（俯卧撑）",
			Purpose: "训练胸大肌、肱三头肌、前三角肌，兼顾肩胛稳定与核心控制",
			Cue:     "双手撑稳，身体成一条直线。胸口靠近双手，再把自己推起来。不要塌腰，也不要耸肩。",
			Actions: []act{
				{Name: "墙壁俯卧撑"},
				{Name: "上斜俯卧撑"},
				{Name: "膝盖俯卧撑"},
				{Name: "半俯卧撑"},
				{Name: "标准俯卧撑"},
				{Name: "窄距俯卧撑"},
				{Name: "偏重俯卧撑"},
				{Name: "单臂半俯卧撑", Lock: true, Extra: "单臂动作力量要求高，默认先锁定。"},
				{Name: "杠杆俯卧撑", Lock: true, Extra: "单臂动作力量要求高，默认先锁定。"},
				{Name: "单臂俯卧撑", Lock: true, Extra: "单臂动作力量要求高，默认先锁定。"},
			},
		},
		{
			Name:    "腿部力量训练（下蹲）",
			Purpose: "训练股四头肌、臀大肌、腘绳肌、小腿，提升下肢力量与髋膝踝协调",
			Cue:     "脚掌踩实，膝盖朝脚尖方向，下蹲时髋先往后坐。腰痛或膝盖刺痛就停。",
			Actions: []act{
				{Name: "肩倒立深蹲", Extra: "肩倒立会压到脖子。如果康复师没有明确让你做，请先在后台锁定这一级。"},
				{Name: "折刀深蹲"},
				{Name: "支撑深蹲"},
				{Name: "半深蹲"},
				{Name: "标准深蹲"},
				{Name: "窄距深蹲"},
				{Name: "偏重深蹲"},
				{Name: "单腿半深蹲"},
				{Name: "单腿辅助深蹲"},
				{Name: "单腿深蹲"},
			},
		},
		{
			Name:    "上肢-拉（引体向上）",
			Purpose: "训练背阔肌、斜方肌、菱形肌、肱二头肌，改善肩胛后缩与下拉控制",
			Cue:     "肩胛先轻轻下沉、向后收，再拉身体。不要用甩的方式借力。可用稳固的门框或单杠。",
			Actions: []act{
				{Name: "垂直引体"},
				{Name: "水平引体"},
				{Name: "折刀引体"},
				{Name: "半引体向上"},
				{Name: "标准引体向上"},
				{Name: "窄距引体向上"},
				{Name: "偏重引体向上"},
				{Name: "单臂半引体向上", Lock: true, Extra: "单臂动作力量要求高，默认先锁定。"},
				{Name: "单臂辅助引体向上", Lock: true, Extra: "单臂动作力量要求高，默认先锁定。"},
				{Name: "单臂引体向上", Lock: true, Extra: "单臂动作力量要求高，默认先锁定。"},
			},
		},
		{
			Name:    "核心训练（举腿）",
			Purpose: "训练腹横肌、腹直肌、腹斜肌、髂腰肌，提升核心稳定与骨盆控制",
			Cue:     "下背尽量贴住支撑面，不要用腰把腿硬拽起来。腰疼立刻停。",
			Actions: []act{
				{Name: "坐姿屈膝"},
				{Name: "平卧抬膝"},
				{Name: "平卧屈举腿"},
				{Name: "平卧蛙举腿"},
				{Name: "平卧直举腿"},
				{Name: "悬垂屈膝", Extra: "这个等级需要稳固单杠。没有单杠时不要做悬垂。"},
				{Name: "悬垂屈举腿", Extra: "这个等级需要稳固单杠。没有单杠时不要做悬垂。"},
				{Name: "悬垂蛙举腿", Extra: "这个等级需要稳固单杠。没有单杠时不要做悬垂。"},
				{Name: "悬垂半直举腿", Extra: "这个等级需要稳固单杠。没有单杠时不要做悬垂。"},
				{Name: "悬垂直举腿", Extra: "这个等级需要稳固单杠。没有单杠时不要做悬垂。"},
			},
		},
		{
			Name:    "后链/脊柱（桥）",
			Purpose: "训练臀大肌、腘绳肌、竖脊肌，强化后链与脊柱伸展能力",
			Cue:     "用臀部把髋抬起来，膝盖不要明显内扣或外翻。不要用腰硬顶。",
			Actions: []act{
				{Name: "短桥"},
				{Name: "直桥"},
				{Name: "高低桥"},
				{Name: "顶桥"},
				{Name: "半桥"},
				{Name: "标准桥"},
				{Name: "下行桥"},
				{Name: "上行桥"},
				{Name: "合桥"},
				{Name: "铁板桥"},
			},
		},
		{
			Name:    "肩部/倒立撑",
			Purpose: "训练三角肌、肱三头肌、肩胛稳定肌，提升肩部力量与倒立能力",
			Cue:     "先在空旷、地面不滑的地方练。头晕、手腕痛、肩痛立刻停。",
			Actions: []act{
				{Name: "靠墙顶立", Reps: -1, Secs: 20},
				{Name: "乌鸦式", Reps: 5},
				{Name: "靠墙倒立", Reps: -1, Secs: 20},
				{Name: "半倒立撑", Reps: 5},
				{Name: "标准倒立撑", Reps: 5, Lock: true, Extra: "倒立撑对手腕和肩的要求很高，默认锁定，旁边最好有保护。"},
				{Name: "窄距倒立撑", Reps: 5, Lock: true, Extra: "倒立撑对手腕和肩的要求很高，默认锁定，旁边最好有保护。"},
				{Name: "偏重倒立撑", Reps: 5, Lock: true, Extra: "倒立撑对手腕和肩的要求很高，默认锁定，旁边最好有保护。"},
				{Name: "单臂半倒立撑", Reps: 5, Lock: true, Extra: "单臂倒立撑默认锁定，必须由康复师解锁。"},
				{Name: "杠杆倒立撑", Reps: 5, Lock: true, Extra: "单臂倒立撑默认锁定，必须由康复师解锁。"},
				{Name: "单臂倒立撑", Reps: 5, Lock: true, Extra: "单臂倒立撑默认锁定，必须由康复师解锁。"},
			},
		},
		{
			Name:    "心肺",
			Purpose: "提升心肺耐力、呼吸控制、代谢与恢复能力",
			Cue:     "保持能说短句的程度，不要憋气冲刺。胸闷、头晕立刻停。",
			Actions: []act{
				{Name: "腹横肌激活呼吸（吸3秒呼5秒）", Reps: 6, Freq: "每天", Extra: "一次呼吸是吸气 3 秒、呼气 5 秒。每组做 6 次这样的呼吸。"},
				{Name: "快走10min", Sets: 1, Reps: -1, Secs: 600, Freq: "每天"},
				{Name: "快走20min", Sets: 1, Reps: -1, Secs: 1200},
				{Name: "椭圆机10min", Sets: 1, Reps: -1, Secs: 600, Freq: "仅康复中心", Extra: "椭圆机在康复中心使用。"},
				{Name: "椭圆机20min", Sets: 1, Reps: -1, Secs: 1200, Freq: "仅康复中心", Extra: "椭圆机在康复中心使用。"},
				{Name: "原地高抬腿30s×3", Reps: -1, Secs: 30, Extra: "如果落地的冲击让你不舒服，就回到快走。"},
				{Name: "快走+椭圆机间歇", Reps: -1, Secs: 180, Freq: "仅康复中心", Extra: "每一组大约 3 分钟，快走和椭圆机按康复师安排交替。"},
				{Name: "跑走结合（1min跑+2min走）×5", Sets: 5, Reps: -1, Secs: 180, Lock: true, Extra: "核心和下肢稳定还没被康复师确认前，跑步默认锁定。每一组是 1 分钟跑加 2 分钟走。"},
				{Name: "慢跑10min", Sets: 1, Reps: -1, Secs: 600, Lock: true, Extra: "核心和下肢稳定还没被康复师确认前，跑步默认锁定。"},
				{Name: "持续慢跑20-30min（需核心下肢测试通过）", Sets: 1, Reps: -1, Secs: 1200, Lock: true, Extra: "至少完成 20 分钟。核心和下肢测试通过前，保持锁定。"},
			},
		},
		{
			Name:    "下肢稳定性",
			Purpose: "训练臀中肌、臀小肌、踝足稳定、本体感觉，改善平衡与骨盆控制",
			Cue:     "旁边可以有稳固的东西扶一下。站不稳就减小难度，不要硬撑。",
			Actions: []act{
				{Name: "金鸡独立（扶墙）30s/侧", Reps: -1, Secs: 30, Side: "双侧", Freq: "每天", Extra: "名字里的剂量是每侧 30 秒。升级默认要完成 3 组，组数可以在后台修改。"},
				{Name: "金鸡独立（不扶）30s/侧", Reps: -1, Secs: 30, Side: "双侧", Extra: "每侧 30 秒。站不稳就回到扶墙的一级。"},
				{Name: "闭眼单脚支撑30s/侧", Reps: -1, Secs: 30, Side: "双侧", Extra: "闭眼前先站稳。每侧 30 秒。"},
				{Name: "闭眼单脚支撑+手臂动作1min/侧", Reps: -1, Secs: 60, Side: "双侧", Extra: "每侧 60 秒。手臂动作由康复师指定。"},
				{Name: "站立式鸟狗3个/侧", Sets: 1, Reps: 3, Side: "双侧", Extra: "名字写的是每侧 3 个，所以这一级目标是 1 组。要改成 3 组的话，在后台改目标组数。"},
				{Name: "神龙摆尾退阶（滑片）5个/侧", Sets: 1, Reps: 5, Side: "双侧", Freq: "仅康复中心", Extra: "需要滑片，适合在康复中心做。每侧 5 个。"},
				{Name: "神龙摆尾3组", Reps: 5, Extra: "名字只写了 3 组，每组次数先按 5 次，康复师可以改。"},
				{Name: "横着走（弹力带，小步）", Reps: -1, Secs: 30, Side: "双侧", Extra: "没有康复师给的弹力带时不要用别的带子凑合。每侧走 30 秒。"},
				{Name: "行进间螃蟹步15m×3", Reps: -1, Side: "无", Extra: "每组走 15 米，走完 3 组、动作标准即可。"},
				{Name: "单腿屈髋训练12×2 / 单侧农夫行走20m×2", Sets: 2, Reps: -1, Side: "双侧", Extra: "两个里面按康复师安排做一个：屈髋每组 12 次，或农夫行走每组 20 米。做完 2 组标准动作即可。"},
			},
		},
		{
			Name:    "臀",
			Purpose: "训练臀大肌、臀中肌、臀小肌，改善髋伸、髋外展与骨盆稳定",
			Cue:     "先把臀部收紧再发力，骨盆不要左右晃。腰疼就减小幅度。",
			Actions: []act{
				{Name: "臀桥（双腿夹砖）3×10", Reps: 10, Extra: "双膝之间可以夹一块瑜伽砖或毛巾。"},
				{Name: "动态臀桥3×7-8", Reps: 7, Extra: "达到每组 7 次即可升级，做到 8 次更好。"},
				{Name: "臀桥顶峰收缩30s×3", Reps: -1, Secs: 30, Extra: "抬到最高处停住，每组 30 秒。"},
				{Name: "单侧臀桥（退阶）3×2-3", Reps: 2, Side: "双侧", Extra: "这是退阶版，每侧每组做到 2 次就可以。"},
				{Name: "单侧臀桥3×6-8", Reps: 6, Side: "双侧", Extra: "每侧每组至少 6 次。"},
				{Name: "单侧臀桥（非支撑腿抬起平行）", Reps: 6, Side: "双侧", Extra: "名字没有写次数，先按每侧每组 6 次，康复师可以改。"},
				{Name: "变式臀桥（左腿内收）", Reps: 6, Side: "双侧", Extra: "按康复师指定的一侧做。系统先按左右都记录。"},
				{Name: "瑜伽球臀桥（康复中心）", Freq: "仅康复中心", Extra: "需要瑜伽球，在康复中心做。"},
				{Name: "坐姿髋外展（弹力带）", Reps: 10, Side: "双侧", Extra: "使用康复师指定的弹力带。"},
				{Name: "单腿臀桥（标准）", Reps: 6, Side: "双侧", Extra: "名字没有写次数，先按每侧每组 6 次。"},
			},
		},
	}
}
