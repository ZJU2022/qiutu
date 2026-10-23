package db

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// contentVersion 变了，程序启动时会对照动作目录。
// 每一级的个数没变时，只更新名称、要点和课上纠正，你标的当前级还在。
// 阶梯本身变了，才会清空进度并重新灌入。这一版来自《训练进阶与退阶》，俯卧撑阶梯按原样保留。
const contentVersion = "doc-and-class-notes"

// act 是灌进数据库之前的一张动作草稿。
// Reps 或 Secs 用 -1 表示“这个动作不记这项”。两个都是 0 时，默认每组 8 次。
type act struct {
	Name      string
	Sets      int
	Reps      int
	Secs      int
	Side      string
	Freq      string
	Lock      bool
	Extra     string
	Points    string
	ClassNote string
}

type mod struct {
	Name    string
	Purpose string
	Cue     string
	Actions []act
}

const baseContra = "出现腰疼、腿麻、电流感、刺痛，或疼痛评分达到 3 分及以上（0 分完全不痛，10 分痛到极点），立即停止。进阶必须同时满足三条：做完 24 小时内腰、臀、腿没有刺痛、麻木或放射感；腰椎不弓背、不顶腰、不旋转，骨盆像一盆水不能洒；呼气发力、吸气还原，不憋气。这里的记录不能代替康复师的诊断。"

func seed(database *sql.DB) error {
	if _, err := database.Exec(`CREATE TABLE IF NOT EXISTS seed_meta (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		version TEXT NOT NULL
	)`); err != nil {
		return fmt.Errorf("建立方案版本表失败: %w", err)
	}
	var version string
	err := database.QueryRow(`SELECT version FROM seed_meta WHERE id = 1`).Scan(&version)
	if err == nil && version == contentVersion {
		return nil
	}
	if err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("读取方案版本失败: %w", err)
	}
	// 阶梯没变时，只把「要点」改回初始文档，并把课上的话挪到单独一列。当前练到第几级保持不动。
	if err == nil && sameShape(database) {
		return refreshNotes(database)
	}

	now := time.Now().Format(time.RFC3339)
	tx, err := database.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var users int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&users); err != nil {
		return fmt.Errorf("检查用户失败: %w", err)
	}
	if users == 0 {
		if _, err := tx.Exec(`INSERT INTO users(id, name, role, created_at) VALUES (1, '训练者', 'user', ?), (2, '康复师', 'therapist', ?)`, now, now); err != nil {
			return fmt.Errorf("写入用户失败: %w", err)
		}
	}

	// 方案换版时清掉旧动作和练习记录。演示数据可以重来，避免旧的 10 级和新的阶梯混在一起。
	for _, table := range []string{
		"workout_set_logs",
		"workout_sessions",
		"symptom_logs",
		"upgrade_logs",
		"user_module_progress",
		"actions",
		"modules",
	} {
		if _, err := tx.Exec(`DELETE FROM ` + table); err != nil {
			return fmt.Errorf("清空 %s 失败: %w", table, err)
		}
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
				frequency, points, class_note, contraindications, video_url, sort_order, is_locked
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, '', ?, ?)`,
				moduleID, level+1, action.Name, sets, reps, secs, side, freq,
				buildPoints(module.Cue, action), action.ClassNote,
				contra(action.Lock, action.Extra),
				level+1, boolInt(action.Lock),
			); err != nil {
				return fmt.Errorf("写入动作 %s 失败: %w", action.Name, err)
			}
		}
	}
	if _, err := tx.Exec(`INSERT INTO seed_meta(id, version) VALUES (1, ?)
		ON CONFLICT(id) DO UPDATE SET version = excluded.version`, contentVersion); err != nil {
		return fmt.Errorf("写入方案版本失败: %w", err)
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

func buildPoints(cue string, a act) string {
	stop := "出现腰疼、腿麻、电流感、刺痛，马上停止。"
	var b strings.Builder
	fmt.Fprintf(&b, "练「%s」时记住：\n%s\n", a.Name, cue)
	b.WriteString("呼吸保持顺畅，不要憋气。\n")
	if a.Extra != "" {
		b.WriteString(a.Extra)
		if !strings.HasSuffix(a.Extra, "\n") {
			b.WriteString("\n")
		}
	}
	b.WriteString(stop)
	return b.String()
}

// lesson 不改要点。要点继续用初始文档。这里只记下第十五到十七课的纠正，并采用课上的组数。
// reps、secs 传 -1 表示课上没规定这项。
func lesson(a act, sets, reps, secs int, lines ...string) act {
	a.Sets = sets
	a.Reps = reps
	a.Secs = secs
	notes := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimPrefix(line, "课上纠正：")
		if line == "" {
			continue
		}
		notes = append(notes, line)
	}
	a.ClassNote = strings.Join(notes, "\n")
	return a
}

func moduleIDs(database *sql.DB) ([]int, error) {
	rows, err := database.Query(`SELECT id FROM modules ORDER BY sort_order, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func sameShape(database *sql.DB) bool {
	modules := catalog()
	ids, err := moduleIDs(database)
	if err != nil || len(ids) != len(modules) {
		return false
	}
	for i, module := range modules {
		var count int
		if err := database.QueryRow(`SELECT COUNT(*) FROM actions WHERE module_id=?`, ids[i]).Scan(&count); err != nil || count != len(module.Actions) {
			return false
		}
	}
	return true
}

func refreshNotes(database *sql.DB) error {
	ids, err := moduleIDs(database)
	if err != nil {
		return err
	}
	tx, err := database.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for i, module := range catalog() {
		for level, action := range module.Actions {
			res, err := tx.Exec(`UPDATE actions SET name=?, points=?, class_note=? WHERE module_id=? AND level=?`,
				action.Name, buildPoints(module.Cue, action), action.ClassNote, ids[i], level+1)
			if err != nil {
				return fmt.Errorf("更新要点失败: %w", err)
			}
			n, err := res.RowsAffected()
			if err != nil {
				return err
			}
			if n != 1 {
				return fmt.Errorf("第 %d 个项目的第 %d 级没有对上", i+1, level+1)
			}
		}
	}
	if _, err := tx.Exec(`INSERT INTO seed_meta(id, version) VALUES (1, ?)
		ON CONFLICT(id) DO UPDATE SET version = excluded.version`, contentVersion); err != nil {
		return err
	}
	return tx.Commit()
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

// step 把康复师写的一个阶梯收成一条动作。
// reps、secs 传 -1 表示文档没有规定这项数字，页面上就显示“按要求做完”。
func step(name, stage string, reps, secs int, side string, lock bool, how, rule, why string) act {
	if side == "" {
		side = "无"
	}
	return act{
		Name:  "【" + stage + "】" + name,
		Reps:  reps,
		Secs:  secs,
		Side:  side,
		Freq:  "每天",
		Lock:  lock,
		Extra: "阶段：" + stage + "\n做法：" + how + "\n要求：" + rule + "\n原理：" + why,
	}
}

func catalog() []mod {
	return []mod{
		{
			Name:    "360 全呼吸",
			Purpose: "用膈肌、腹横肌和盆底建立腹内压，给后面的核心动作打底",
			Cue:     "吸气时胸廓向四周扩开，呼气时轻轻收拢。不要只顶肚子，也不要耸肩。",
			Actions: []act{
				lesson(act{Name: "360 全呼吸", Freq: "每天", Extra: "阶段：每天的第一项。\n做法：初始方案只写了「360全呼吸」。\n要求：文档没有写次数，也没有写分解步骤。\n原理：文档把它放在所有动作前面。"}, 1, 10, -1,
					"课上纠正：",
					"每组 10 次，不要多做。",
					"组间休息 3 到 5 分钟，可以放松。",
					"穿插在别的训练中间，做一组就够。",
				),
			},
		},
		{
			Name:    "放松拉伸",
			Purpose: "松开臀肌和大腿内侧、外侧、后侧，减少腰椎被迫跟着动",
			Cue:     "牵拉处可以有拉开的感觉。出现刺痛、麻木或往腿上窜的感觉，立刻减轻幅度。",
			Actions: []act{
				lesson(act{Name: "放松拉伸：臀肌、大腿内侧、外侧、后侧", Freq: "每天", Extra: "阶段：每天的热身。\n做法：初始方案写的是放松拉伸臀肌，以及大腿内侧、外侧、后侧。\n要求：文档没有写每一处的手法和秒数。\n原理：先把这几处松开，再做后面的稳定训练。"}, 1, -1, -1,
					"课上纠正：",
					"不用每次训练都松解。每周自己至少松解 1 到 2 次。",
					"顺序是：松解，训练，松解，拉伸。",
					"松解臀中肌：侧躺。上面那只脚跨到下面那只脚前面，脚贴地，不要发力。来回滚 20 到 30 次。",
					"松解臀大肌、大腿后侧、小腿：用单腿或单侧臀部躺到滚轴上，再开始松。",
					"松解大腿外侧：滚阔筋膜张肌。",
					"松解肌腱：课上还没写具体做法。",
					"拉伸髂腰肌：课上还没写具体做法。",
					"拉伸臀肌：课上还没写具体做法。",
					"拉伸后侧链：课上标了问号，做法还没定。",
				),
			},
		},
		{
			Name:    "死虫式",
			Purpose: "激活腹横肌，抗伸展，建立腹内压",
			Cue:     "仰卧，腰死死贴地。腰和地面之间出现缝，就说明这一级还太难。",
			Actions: []act{
				lesson(step("静态死虫", "退阶", -1, -1, "无", false,
					"仰卧，四肢朝天，腰部死死压地。只做呼吸，不放下任何肢体。",
					"腰和地面之间没有缝。这一级只练呼吸和腰贴地，不放下手脚。",
					"先让腹横肌学会撑住腹内压。手脚先不动，腰椎才不会被四肢的重量拽离地面。"),
					3, -1, 40,
					"课上纠正：",
					"双腿夹一个瑜伽球，逼着肚子和核心出力。",
					"也可以在双腿之间、两只手腕之间各放一块瑜伽砖，向中间用力夹。",
					"三组都能超过 40 秒，再考虑进到下一级。",
				),
				step("仰卧脚跟滑行", "退阶", -1, -1, "双侧", false,
					"吸气准备，呼气时一侧脚沿地面缓慢滑出，吸气收回。",
					"腰部不能离地。一次只动一条腿。",
					"腿滑出去会把腰椎往前拉。腹横肌要在这个小范围里保住腰贴地，这叫抗伸展。"),
				step("单侧死虫", "退阶", -1, -1, "双侧", false,
					"只放下一侧手臂，或只放下一侧腿，不同时放对侧手脚。",
					"腰部仍然没有缝。不要左右两边一起放。",
					"只动一侧，力矩比标准死虫小。能稳住之后，再让对侧手脚一起走。"),
				step("标准死虫", "标准", 10, -1, "双侧", false,
					"仰卧，腰部死死贴地。呼气时同时缓慢放下一侧手臂和对侧腿，吸气收回。",
					"每侧 10 次，连续 3 组。做完 24 小时内没有脚麻，腰痛没有加重。动作中腰一离地，就回到退阶。",
					"对侧手脚同时远离身体，腹横肌要对抗这个拉开的力量。这是这一项的标准版。"),
				step("抗阻死虫", "进阶", -1, -1, "双侧", false,
					"手脚对抗弹力带，或对抗墙面来做死虫。",
					"只有标准版完美，并且 24 小时内无脚麻、腰痛不加重，才做这一级。腰离地就停。",
					"外加的阻力让腹横肌在更大力矩下继续抗伸展。"),
				step("交叉死虫", "进阶", -1, -1, "双侧", false,
					"手脚放下的位置更深，甚至轻触地面后再收回。",
					"幅度可以加大，腰仍然不能离地。",
					"肢体放得越低，力臂越长，腹横肌要付出的力越大。"),
			},
		},
		{
			Name:    "臀桥",
			Purpose: "激活臀大肌，让它代替腰椎把髋抬起来",
			Cue:     "脚跟蹬地，臀部发力。肩膀到膝盖成一条直线就停，不要顶腰。",
			Actions: []act{
				step("静态臀桥（微幅）", "退阶", -1, 5, "无", false,
					"只把臀部顶离地面 1 到 2 厘米，夹紧臀部保持，再放下。",
					"不追求高度。每组保持 5 秒。",
					"很小的幅度就能感到臀大肌，同时几乎不挤压腰椎。"),
				step("仰卧骨盆后倾", "退阶", -1, -1, "无", false,
					"仰卧屈膝，不做臀桥。尾骨向上卷，腰贴地，感受臀部收紧。",
					"只要感到臀部收紧、腰贴地即可。",
					"先学会骨盆后倾，是为了后面抬髋时用臀，而不是把腰顶硬。"),
				lesson(step("双腿臀桥", "标准", 15, -1, "无", false,
					"脚跟蹬地，臀部发力把身体顶起。肩膀到膝盖成一条直线的瞬间即停。最高点夹紧臀部 2 秒。",
					"能轻松完成 15 次，连续 3 组。顶峰时腰没有挤压感、没有酸痛。不要顶腰。双腿还做不到这样，就不要做单腿。",
					"臀大肌负责伸髋。直线一出现就停，是为了不让腰椎继续后伸去“帮”臀部。"),
					3, 15, -1,
					"课上纠正：",
					"吸气向上，吐气向下。全程屁股收紧。",
					"上撑不用到最高，身体成一条直线就停。",
					"双腿夹瑜伽砖或瑜伽球，膝盖不要向外撇，也不要向内扣。",
					"大小腿大约 75 度。角度再大，练到的主要是大腿后侧；角度更小，练到的主要是臀。",
					"下放时屁股先不贴地。双脚朝前，两只脚平行。",
				),
				step("单腿臀桥", "进阶", -1, -1, "双侧", false,
					"一侧腿伸直抬起，只用支撑侧的腿完成臀桥。",
					"骨盆不能一边高一边低。出现倾斜就退回双腿臀桥。",
					"单腿时臀大肌和臀中肌要同时保住骨盆水平。倾斜说明支撑侧还不够。"),
				step("垫高双脚臀桥", "进阶", -1, -1, "无", false,
					"双脚踩在沙发或矮凳上，再做臀桥。",
					"仍然是臀部发力，到身体成直线就停，不顶腰。",
					"脚垫高以后，髋要走更长的一段，臀大肌的工作幅度更大。"),
				step("负重臀桥", "进阶", -1, -1, "无", true,
					"髋部放一个重物，例如书包，再做双腿臀桥。",
					"前提是核心能保持稳定，双腿臀桥已经完美。这一级默认锁定，康复师确认后才解锁。",
					"外加重量会放大任何顶腰。核心稳不住时，重量会直接进到腰椎。"),
			},
		},
		{
			Name:    "侧桥",
			Purpose: "抗侧屈，强化腰方肌和臀中肌",
			Cue:     "髋部抬起后，身体成一条直线。骨盆不要下沉，也不要向前或向后翻。",
			Actions: []act{
				lesson(step("屈膝侧桥", "退阶", -1, -1, "双侧", false,
					"侧卧，肘撑地，双膝弯曲，膝盖和小腿着地，把髋部推离地面。",
					"身体从膝盖到肩膀成一条直线。",
					"屈膝缩短了力臂，腰方肌和臀中肌更容易把骨盆托住。"),
					3, -1, 60,
					"课上纠正：先做这一级。",
					"3 组，每组 1 分钟。",
					"撑不住不要硬撑。有一点酸就停。",
				),
				step("靠墙侧桥", "退阶", -1, -1, "双侧", false,
					"背靠墙做侧桥，用墙告诉自己骨盆有没有向前或向后翻。",
					"背和墙的关系保持稳定，不要转成正面或背面贴墙。",
					"墙提供本体感觉：身体一旋转，背就会离开墙。"),
				lesson(step("标准侧桥", "标准", -1, 20, "双侧", false,
					"侧卧，肘撑地，双脚并拢伸直，身体从脚到肩膀呈一条直线。",
					"训练可以从每侧 20 秒做起。要升级，需每侧轻松保持 45 到 60 秒，骨盆不下沉、不前后翻转。腰方肌没有明显酸痛，训练后没有脚麻。",
					"直腿侧桥的力臂更长，腰方肌抗侧屈，臀中肌不让骨盆掉下去。"),
					3, -1, 20,
					"课上纠正：这是腿打直的那一档。",
					"每侧 20 秒。",
					"上半身放松，力撑在侧腰上。",
					"身体如果向后倒，往前靠一点。",
				),
				step("动态侧桥", "进阶", -1, -1, "双侧", false,
					"在最高点缓慢放下髋部，但不触地，再顶起。",
					"速度必须极慢，不能借惯性。全程躯干不旋转。",
					"慢速起落让肌肉在整个幅度里控制骨盆，而不是靠甩一下上去。"),
				step("侧桥抬腿", "进阶", -1, -1, "双侧", false,
					"在标准侧桥上，把上方的腿向上抬起，再放下。",
					"抬腿时躯干仍然是一条直线，不旋转。",
					"上方腿抬起会加重上方臀中肌的工作，同时要求下面的侧腰不要塌。"),
				step("侧桥屈髋", "进阶", -1, -1, "双侧", false,
					"在屈膝侧桥的基础上，上方的腿缓慢向前屈髋，像高抬腿，然后回正。",
					"针对腹内外斜肌。腿向前时，胸口不要跟着转。",
					"屈髋加上侧桥，腹内外斜肌要抗住旋转。这是侧向稳定和旋转控制叠在一起。"),
			},
		},
		{
			Name:    "平板支撑",
			Purpose: "抗伸展，建立整体核心刚性",
			Cue:     "肘撑地，身体成直线。收紧臀部，骨盆微微后倾，腰背是平的，甚至可以微微拱起。不要塌腰。",
			Actions: []act{
				step("跪姿平板", "退阶", -1, -1, "无", false,
					"双膝着地，小腿抬起，其余和标准平板一样。",
					"肩、髋、膝成一条线。不要塌腰，也不要把屁股撅得很高。",
					"跪姿缩短力臂，腹肌用来抗伸展的负担更小。"),
				step("斜面平板", "退阶", -1, -1, "无", false,
					"手撑在沙发、床沿或墙面上，身体呈斜线。",
					"身体是直的。越靠近竖直，越轻松。",
					"手的位置越高，体重压到核心上的水平力量越小。"),
				lesson(step("标准平板", "标准", -1, 120, "无", false,
					"肘撑地，身体成直线。主动收紧臀部，骨盆微微后倾。",
					"从 15 秒开始。要升级，需保持 2 分钟，中途没有塌腰，骨盆没有前倾。",
					"身体成桥时，重力想让腰椎塌下去。核心刚性就是不让这件事发生。"),
					3, -1, 120,
					"课上纠正：",
					"双臂把身体撑起来，不要塌下去。",
					"收紧核心和屁股。",
					"静态能撑到 90 到 120 秒，再试动态：不停地抬起一条腿。",
					"能撑满 2 分钟，再考虑再进一级。",
				),
				lesson(step("平板支撑单腿抬起", "进阶", -1, -1, "双侧", false,
					"标准平板上，交替把一条腿抬起，只抬 5 到 10 厘米。",
					"身体纹丝不动。如果左右晃，或者腰往下塌，退回标准平板。",
					"抬腿会拿走一个支撑点。核心必须抗住由此出现的旋转和伸展。"),
					3, -1, -1,
					"课上纠正：这是变式平板。",
					"收紧核心和屁股。",
					"下放时放慢，不要着急。",
					"下放时后背要撑住，不要塌。",
					"不停地抬起一条腿。",
				),
				step("平板单手摸肩", "进阶", -1, -1, "双侧", false,
					"交替用手去摸对侧肩膀。",
					"禁止身体左右晃动。",
					"手一离开地面，身体就想转。这是在平板里加抗旋转。"),
				step("不稳定平面平板", "进阶", -1, -1, "无", false,
					"手肘或双脚放在平衡垫或瑜伽球上做平板。",
					"只有标准平板能保持 2 分钟完美姿态，才做这一级。身体一晃就下来。",
					"不稳定的支撑让深层肌肉不断做小修正，前提是标准姿势已经很稳。"),
			},
		},
		{
			Name:    "鸟狗式",
			Purpose: "激活多裂肌，抗旋转，练习动态控制",
			Cue:     "四足跪姿，核心先收紧。骨盆保持水平，背部平稳，腰椎不要侧弯或塌陷。",
			Actions: []act{
				lesson(step("只伸腿", "退阶", 5, -1, "双侧", false,
					"四足跪姿，只向后伸一条腿。",
					"骨盆不能晃动。",
					"先单独练伸腿，把“腿动、骨盆不动”从伸手分开。"),
					3, 5, -1,
					"课上纠正：现在先退到这一级。",
					"3 组，每组 5 到 8 个。",
					"手或脚出去之前，骨盆轻轻卷一下，不要卷到底，让腰放平。",
					"出去时收紧核心。腰一塌，或者骨盆一晃，就是没收住。",
					"正常伸出去就好，感受身体向远处长。可以先站着收紧核心、双手上举，找这个感觉。",
				),
				lesson(step("只伸手", "退阶", 5, -1, "双侧", false,
					"四足跪姿，只向前伸一条手臂。",
					"躯干不动。",
					"手臂前伸容易带着上背旋转。这一级只处理这件事。"),
					3, 5, -1,
					"课上纠正：现在先退到这一级。",
					"3 组，每组 5 到 8 个。",
					"伸手之前，骨盆轻轻卷一下，不要卷到底，让腰放平。",
					"手出去时收紧核心，骨盆不要晃，腰不要塌。",
				),
				step("静态保持", "退阶", -1, 5, "双侧", false,
					"手脚伸出后停住，感受核心收紧。",
					"每侧保持 5 秒。骨盆仍然水平。",
					"停住的这几秒，是多裂肌和腹肌在做静态控制，而不是靠甩出去。"),
				lesson(step("标准鸟狗式", "标准", -1, 8, "双侧", false,
					"核心预先收紧。先伸腿，稳定后再伸对侧手臂，然后保持。",
					"每侧保持 8 到 10 秒。骨盆没有翻转，腰椎没有侧弯或塌陷。做不到就不要进入后面的动态或阻力。",
					"对侧手脚伸开时，身体想转。多裂肌负责节段稳定，腹肌负责不让躯干旋转。"),
					3, -1, 8,
					"课上纠正：这是中高级。退阶那两级还做不稳时，先不要做这一级。",
					"双手撑好。手或脚出去前，骨盆轻轻卷一下，不要卷到底。",
					"全程收紧核心。出去时腰不要塌，骨盆不要晃。",
					"正常伸，感受身体向远处长。",
				),
				step("手膝触碰", "进阶", -1, -1, "双侧", false,
					"对侧手脚伸出后，收回时在腹部下方轻触，再伸出。",
					"感觉腰在扭，立刻停止。",
					"收回和再伸出增加了一次控制，不能靠惯性荡回来。"),
				step("弹力带鸟狗式", "进阶", -1, -1, "双侧", false,
					"手脚对抗弹力带的阻力做鸟狗。",
					"只有标准版完美才加阻力。腰一扭就停。",
					"阻力放大了旋转力矩，多裂肌和腹斜肌要付出更多力来保持不动。"),
				step("画圈鸟狗式", "进阶", -1, -1, "双侧", false,
					"伸出的手脚在空中缓慢画圈。",
					"画圈时骨盆仍然不能晃。",
					"小圆改变着力的方向，神经控制要比直线伸出更细。"),
			},
		},
		{
			Name:    "屈髋训练",
			Purpose: "学会髋往后折，脊柱像一根棍子一样不动",
			Cue:     "屈髋是屁股向后推，不是弯腰。腰椎不屈曲、不反弓。",
			Actions: []act{
				step("靠墙屈髋", "退阶", -1, -1, "无", false,
					"背对墙站立，距离墙约一脚长。微屈膝，用臀部去碰到后面的墙，再夹紧臀部站起。",
					"全程腰背挺直。膝盖不能向前顶，小腿几乎垂直于地面。",
					"墙给你触觉：臀部碰到墙，说明髋真的向后了。这是在学向后推臀，不是向下弯腰。"),
				step("木棍髋铰链", "退阶", -1, -1, "无", false,
					"晾衣杆或扫把贴住后脑勺、胸椎中段（肩胛骨之间）、骶骨三个点。微屈膝，臀部向后推。",
					"下放和起身时，三个接触点都不能离开身体。木棍离开胸口或后脑勺，说明弓背了，立刻停止。",
					"三个点在一条直线上，就是腰椎既没有屈曲也没有过度后伸。"),
				step("手扶椅背屈髋", "退阶", -1, -1, "无", false,
					"双手轻轻扶着椅背或门框，体会臀部向后推。",
					"手只维持平衡，不能用上肢拉拽借力。",
					"平衡的要求降低以后，注意力才能放在髋关节本身。"),
				lesson(step("徒手髋铰链", "标准", 12, -1, "无", false,
					"双脚与肩同宽，微屈膝，臀部向后推，直到大腿后侧有明显拉伸感，或躯干与地面约成 45 度。然后用脚蹬地，夹臀站起。",
					"全程不弓背、不反弓。",
					"这是髋铰链的标准版。感觉应该在大腿后侧，而不是腰被折弯。"),
					3, 12, -1,
					"课上纠正：这是单腿屈髋的退阶。",
					"3 组，每组 12 到 15 次。要感到大腿后侧和屁股在用力。",
					"弹力带绑住双膝。膝盖容易内扣，全程把膝盖向外撑开。",
					"双手自然垂着，跟着屈髋放下。",
					"屁股向后顶，但别顶太多，腰会不自觉弓起来。",
					"先收紧核心，再把屁股向后推。脊柱整根跟着走。",
					"上半身和下半身的夹角，等于膝盖弯曲的角度。",
				),
				step("高脚杯深蹲过渡", "进阶", -1, -1, "无", true,
					"双手把壶铃或重书包抱在胸前。下蹲时先屈髋，再屈膝，蹲到一半时站起。",
					"必须标准版完美，且训练后 24 小时没有腰腿痛、脚麻没有加重。重量在胸前，帮助躯干保持直立。这一级默认锁定。",
					"这是从纯屈髋过渡到下肢一起发力。负荷会放大代偿，所以先锁着。"),
			},
		},
		{
			Name:    "下肢稳定性训练",
			Purpose: "单腿站稳时骨盆保持水平，减轻腰椎代偿",
			Cue:     "两侧骨盆一样高。不要靠挺肚子来找平衡。支撑脚的脚趾抓地。",
			Actions: []act{
				step("扶墙或扶椅单腿站立", "退阶", -1, -1, "双侧", false,
					"单手轻扶墙或椅背，另一条腿屈膝抬起，大腿与地面平行。支撑侧脚尖朝前。",
					"可以闭眼体会支撑侧臀部，尤其是侧臀的酸胀。骨盆完全水平。",
					"手把平衡的难度接走，注意力才能放在臀中肌，而不是慌着不摔倒。"),
				step("靠墙单腿站立", "退阶", -1, -1, "双侧", false,
					"后背贴墙，双脚离墙约一脚。单腿站立，支撑腿缓慢屈膝向下滑几厘米，再蹬起。",
					"后背和臀部全程贴墙，腰椎不能离开墙面。",
					"墙挡住了骨盆后移和腰椎代偿，股四头肌和臀大肌更容易被用到。"),
				step("分腿重心转移", "退阶", -1, -1, "双侧", false,
					"双脚前后站立，像走路的弓步。慢慢把重心移到前腿，后腿脚尖轻点地。",
					"感受前腿臀部发力。",
					"后脚还点着地，比单腿悬空安全，同时已经在练单侧承重。"),
				lesson(step("单侧腿下蹲", "标准", -1, -1, "双侧", false,
					"初始方案的下肢稳定训练里没有这一条。",
					"做法以课上纠正为准。",
					"它是课上加进来的单腿下蹲，不写进初始文档。"),
					3, -1, -1,
					"课上纠正：",
					"屈髋小一点。屁股向后靠一点就行。往下的过程中再屈髋，方向是向后、向下。",
					"弹力带绑住双膝。膝盖容易内扣，全程把膝盖向外撑开。",
				),
				lesson(step("徒手光脚单脚站立", "标准", -1, 30, "双侧", false,
					"赤脚单腿站立，另一侧屈膝抬起，大腿与地面平行。",
					"每侧保持 30 到 60 秒。两侧骨盆一样高，不要靠挺肚子来找平衡，脚趾抓地。",
					"这是单腿稳定的标准版。骨盆一歪，腰椎就要出来帮忙。"),
					3, -1, 30,
					"课上纠正：先练睁眼。闭眼要有人看着，不要自己单独闭眼练。",
					"大拇指踩住一条弹力带。另一只脚抬起，大腿和小腿成 90 度。",
					"先把背打直，双手向前伸，把背拉平，再屈一点髋。",
					"3 组，每组 30 秒。慢慢加到 40 秒，然后再试闭眼。",
					"人站在中间，不要向另一边偏。不要耸肩。",
				),
				step("单腿站立摸脚趾", "标准", -1, -1, "双侧", false,
					"单腿站立，脊柱中立，支撑侧屈髋，身体前倾，用对侧手去摸支撑侧脚踝，再站直。",
					"全程骨盆平稳，腰椎不扭转。",
					"这是单腿支撑加上屈髋，接近单腿时弯下去穿鞋或走路摆腿的情况。"),
				lesson(step("闭眼单脚站立", "进阶", -1, -1, "双侧", false,
					"在徒手单脚站立稳定之后，闭上眼睛再做。",
					"睁眼版已经能保持，并且训练后 24 小时无脚麻、无腰痛，才做这一级。",
					"去掉视觉以后，只能靠脚底和关节的感觉来维持骨盆水平。"),
					3, -1, 30,
					"课上纠正：",
					"睁眼的 30 秒还不稳定时，不要做这一级。",
					"闭眼必须有人看着。",
				),
				step("不稳定平面单腿站立", "进阶", -1, -1, "双侧", false,
					"站在折叠的厚毛巾、平衡垫或小枕头上做单腿站立。",
					"同样要先通过标准版，训练后 24 小时无脚麻、无腰痛。",
					"地面微小的不平，考验踝关节和臀中肌的反应。"),
				lesson(act{Name: "踝泵", Freq: "每天", Extra: "初始方案没有踝泵。足下垂怎么处理，见课上纠正。"}, 3, -1, -1,
					"课上纠正：",
					"现在有一点足下垂，要多做踝泵。",
					"踝泵就是脚尖向上勾、再向下踩，来回活动脚踝。",
					"足背屈打不开，后面下蹲会别扭。",
				),
			},
		},
		{
			Name:    "髋关节灵活性训练",
			Purpose: "只让大腿骨在髋里转动，腰椎像浇在水泥里",
			Cue:     "中央型突出且神经敏感时，90/90 的铁律是：腰椎不动，只有大腿骨在转。为了贴地而骨盆后倾、腰椎弓起，是红线。",
			Actions: []act{
				step("仰卧静态 4 字拉伸", "退阶", -1, 30, "双侧", false,
					"仰卧，双腿屈膝。一侧脚踝搭在对侧膝盖上，呈 4 字。双手抱住下方大腿，缓慢拉向胸口。",
					"腰部死死贴地。感受臀部深层的拉伸。每侧保持 30 秒。",
					"做不了 90/90 时，常常是髋外旋肌群太紧。先在没有腰椎压力的仰卧位把这层松开，里面包括梨状肌。"),
				lesson(step("仰卧单侧主动 90/90", "退阶", -1, -1, "双侧", false,
					"仰卧，一条腿屈膝踩地。另一条腿抬起，髋和膝都呈 90 度。只靠大腿根把这条腿向外打开，再向内收。",
					"双手摸着骨盆，骨盆不能跟着腿左右晃。每侧 10 到 15 次。",
					"仰卧时重力最小，用来切断腰跟着扭的习惯，让大脑只控制髋的旋转。"),
					3, -1, -1,
					"课上纠正：",
					"平躺。双腿夹球。小腿和大腿 90 度，肚子和大腿也是 90 度。",
					"双腿向下用力，但不要在地面上划。屁股收紧，让屁股和大腿一起紧。",
					"腰贴地。一只手垂直向上举。",
					"一个气球分三次吹完，每次中间停 3 秒。吹的时候双腿和屁股收紧，举起的手再往上送。",
					"这样做是为了增加腹内压。",
				),
				step("坐姿 90/90 动态切换", "标准", 10, -1, "无", false,
					"坐在地面，双腿呈 90/90。双手向前平举，不撑地。核心收紧，双腿交替切换，身体微微前倾。",
					"连续 10 次以上，腰背挺直，没有任何腰部不适，才可以进阶。禁止屁股眨眼：骨盆向后翻、腰椎弓起就要降阶。肩膀和胸廓朝前，不能跟着腿转。动作要慢。",
					"坐姿去掉了手的支撑。腹肌和髋外旋、内旋肌要自己保住骨盆，不把旋转漏给腰椎。"),
				step("90/90 起立", "进阶", -1, -1, "双侧", false,
					"从地面 90/90 的坐姿开始，不用手撑。前腿脚掌踩实，核心收紧，用臀和腿把身体推起来。",
					"坐姿切换已经能做 10 次且腰背挺直，才做这一级。起身时背部不能弓。",
					"这是单腿稳定和髋旋转合在一起。重心转移时，腰椎仍然要保持刚性。"),
			},
		},
		{
			Name:    "胸肌训练（俯卧撑）",
			Purpose: "保留的上肢动作：胸大肌、肱三头肌、前三角肌，加上肩胛和核心",
			Cue:     "双手撑稳，身体成一条直线。胸口靠近双手，再把自己推起来。不要塌腰，也不要耸肩。",
			Actions: []act{
				lesson(act{Name: "墙壁俯卧撑"}, 3, -1, -1,
					"课上纠正：",
					"双手微微向内转。收紧核心和屁股。",
					"撑起来以后，脚后跟大约落在手臂末端那个远近。这就是起始姿势。",
					"标准俯卧撑身体接近一条横线，可以想成 180 度。墙壁俯卧撑大约是 90 度。靠床做是一个钝角。",
					"要感到胸口外侧和手臂在用力。",
					"做 3 组。每组有一点酸就停，不规定次数。",
				),
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
	}
}
