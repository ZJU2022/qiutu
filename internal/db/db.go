// Package db 负责打开数据库、建表，以及灌入康复师给出的动作方案。
// 数据库可以想成一个会保存的笔记本：程序关掉，本子还在。
package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// Open 打开（没有就创建）一个 SQLite 数据库文件，并保证表和种子数据存在。
func Open(path string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("创建数据目录失败: %w", err)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("确定数据库路径失败: %w", err)
	}
	// 每个新连接都会带上这些设置。外键让“动作必须属于某个模块”真正生效。
	dsn := "file:" + filepath.ToSlash(abs) + "?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"
	database, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败: %w", err)
	}
	if err := migrate(database); err != nil {
		_ = database.Close()
		return nil, err
	}
	if err := seed(database); err != nil {
		_ = database.Close()
		return nil, err
	}
	return database, nil
}

func migrate(database *sql.DB) error {
	// 表结构按产品文档第 8 节。少数额外字段在注释里说明了原因。
	_, err := database.Exec(`
CREATE TABLE IF NOT EXISTS users (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL,
    role TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS modules (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL,
    purpose TEXT NOT NULL,
    sort_order INTEGER NOT NULL,
    is_locked INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS actions (
    id INTEGER PRIMARY KEY,
    module_id INTEGER NOT NULL REFERENCES modules(id),
    level INTEGER NOT NULL,
    name TEXT NOT NULL,
    target_sets INTEGER NOT NULL DEFAULT 3,
    target_reps INTEGER,
    target_seconds INTEGER,
    target_side TEXT NOT NULL DEFAULT '无',
    frequency TEXT NOT NULL DEFAULT '隔天',
    points TEXT,
    contraindications TEXT,
    video_url TEXT,
    sort_order INTEGER NOT NULL,
    is_locked INTEGER NOT NULL DEFAULT 0,
    UNIQUE(module_id, level)
);

CREATE TABLE IF NOT EXISTS user_module_progress (
    id INTEGER PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users(id),
    module_id INTEGER NOT NULL REFERENCES modules(id),
    current_level INTEGER NOT NULL DEFAULT 1,
    max_level INTEGER NOT NULL DEFAULT 1,
    status TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE(user_id, module_id)
);

CREATE TABLE IF NOT EXISTS workout_sessions (
    id INTEGER PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users(id),
    module_id INTEGER NOT NULL REFERENCES modules(id),
    action_id INTEGER NOT NULL REFERENCES actions(id),
    date TEXT NOT NULL,
    status TEXT NOT NULL,
    total_sets INTEGER NOT NULL DEFAULT 0,
    symptom_flag INTEGER NOT NULL DEFAULT 0,
    note TEXT,
    created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS workout_set_logs (
    id INTEGER PRIMARY KEY,
    session_id INTEGER NOT NULL REFERENCES workout_sessions(id),
    set_no INTEGER NOT NULL,
    reps INTEGER NOT NULL DEFAULT 0,
    seconds INTEGER NOT NULL DEFAULT 0,
    side TEXT NOT NULL,
    rpe INTEGER NOT NULL,
    pain_score INTEGER NOT NULL,
    symptom_type TEXT NOT NULL,
    form_ok INTEGER NOT NULL,
    form_quality TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS symptom_logs (
    id INTEGER PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users(id),
    date TEXT NOT NULL,
    body_part TEXT NOT NULL,
    symptom_type TEXT NOT NULL,
    severity INTEGER NOT NULL,
    note TEXT,
    created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS upgrade_logs (
    id INTEGER PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users(id),
    module_id INTEGER NOT NULL REFERENCES modules(id),
    action_id INTEGER NOT NULL,
    from_level INTEGER NOT NULL,
    to_level INTEGER NOT NULL,
    type TEXT NOT NULL,
    reason TEXT,
    source_session_id INTEGER,
    created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_actions_module ON actions(module_id, level);
CREATE INDEX IF NOT EXISTS idx_progress_user ON user_module_progress(user_id, module_id);
CREATE INDEX IF NOT EXISTS idx_sessions_user_date ON workout_sessions(user_id, date);
CREATE INDEX IF NOT EXISTS idx_sets_session ON workout_set_logs(session_id, set_no);
`)
	if err != nil {
		return fmt.Errorf("建表失败: %w", err)
	}
	if err := ensureColumn(database, "actions", "class_note", "TEXT"); err != nil {
		return err
	}
	return nil
}

// ensureColumn 给已经存在的表补一列。SQLite 没有「有了就跳过」的写法，所以先看表结构。
func ensureColumn(database *sql.DB, table, column, decl string) error {
	rows, err := database.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return fmt.Errorf("查看表结构失败: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid, notNull, pk int
		var name, typ string
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notNull, &dflt, &pk); err != nil {
			return err
		}
		if name == column {
			return nil
		}
	}
	if _, err := database.Exec(`ALTER TABLE ` + table + ` ADD COLUMN ` + column + ` ` + decl); err != nil {
		return fmt.Errorf("增加 %s 失败: %w", column, err)
	}
	return nil
}
