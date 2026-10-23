// 这一页给康复师看训练思路。数据仍交给后面的接口保存。

const app = document.getElementById("app");
const state = {
  home: null,
  ladders: {},
  history: [],
  workout: null,
  result: null,
  warning: "",
  error: "",
  notice: "",
  rollbackReason: "",
  symptom: { body_part: "腰", symptom_type: "腿麻", severity: 3, note: "" },
  adminModuleId: 1,
  adminActions: [],
  muscle: "",
  editingId: 0,
};

function esc(value) {
  return String(value ?? "").replace(/[&<>"']/g, (ch) => ({
    "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;",
  }[ch]));
}

async function api(path, options = {}) {
  const headers = { ...(options.headers || {}) };
  if (options.body) headers["Content-Type"] = "application/json";
  const response = await fetch(path, { ...options, headers });
  const data = await response.json().catch(() => ({}));
  if (!response.ok) throw new Error(data.error || "请求失败");
  return data;
}

function targetText(action) {
  const bits = [action.target_sets + " 组"];
  if (action.target_reps) bits.push("每组 ≥ " + action.target_reps + " 次");
  if (action.target_seconds) bits.push("每组 ≥ " + action.target_seconds + " 秒");
  if (!action.target_reps && !action.target_seconds) bits.push("按要求做完");
  if (action.target_side && action.target_side !== "无") bits.push(action.target_side);
  return bits.join("，");
}

function statusText(action) {
  if (action.state === "current") return "当前";
  if (action.state === "done") return "已通过";
  if (action.state === "locked") return "锁定";
  return "未解锁";
}

async function load() {
  state.home = await api("/api/modules?user_id=1");
  const details = await Promise.all(
    state.home.modules.map((item) => api("/api/modules/" + item.id + "/actions?user_id=1")),
  );
  details.forEach((detail) => {
    state.ladders[detail.module.id] = detail;
  });
}

async function loadAdmin() {
  const data = await api("/api/admin/actions?module_id=" + state.adminModuleId, {
    headers: { "X-Demo-Role": "admin" },
  });
  state.adminActions = data.actions || [];
}

function render() {
  if (!state.home) {
    app.innerHTML = "<p>正在读取方案…</p>";
    return;
  }
  app.innerHTML = [
    renderHead(),
    renderAnatomy(state.muscle),
    state.error ? `<div class="box warn">${esc(state.error)}</div>` : "",
    state.notice ? `<div class="box ok">${esc(state.notice)}</div>` : "",
    state.warning ? `<div class="box warn"><b>请先停下来。</b> ${esc(state.warning)}</div>` : "",
    renderSuggest(),
    renderModules(),
  ].join("");
}

function renderHead() {
  return `<h1>康复自重训练方案</h1>
    <p>这一页按康复师写的进退阶排列。动作下面的要点按初始方案来写。课上纠正是第十五到十七课单独的一列。每天从里面选 3 到 5 个。黄色那一行是你标的当前级。</p>
    <div class="rules">
      <div><strong>现在的判断</strong>核心很有力，但还不知道怎么用，控制不够。动作保持简单、直接：该收紧的地方收紧，不要再加别的小动作。</div>
      <div><strong>停止</strong>疼痛达到 3 分，或出现腰疼、腿麻、电流感、刺痛，立刻停止。骨盆像一盆水，不能洒。</div>
      <div><strong>呼吸</strong>大多数动作呼气发力、吸气还原。臀桥按课上的说法：吸气向上，吐气向下。</div>
      <div><strong>当前级</strong>每个项目旁边可以自己选「我现在在第几级」，选好后点设成这一级。黄色行会跟着变。</div>
      <div><strong>修改</strong>每个动作后面的「编辑」可以改名字、要点、课上纠正、组数、次数和秒数。改完点保存。</div>
    </div>`;
}

function renderSuggest() {
  const home = state.home;
  if (home.rest_today) return `<p><b>今天：</b>${esc(home.rest_message)}</p>`;
  const items = (home.suggestions || []).map((item) =>
    `<a href="#m-${item.module_id}">${esc(item.module_name)} 第${item.level}级 ${esc(item.action_name)}</a>`,
  );
  return `<p><b>今天建议先看：</b>${items.join("；") || "今天先休息。"}</p>`;
}

function findAction(id) {
  for (const detail of Object.values(state.ladders)) {
    const found = (detail.actions || []).find((item) => item.id === id);
    if (found) return found;
  }
  return null;
}

function renderActionRow(action) {
  if (state.editingId === action.id) {
    return `<tr class="${esc(action.state)}">
      <td>${action.level}</td>
      <td class="edit-fields">
        <label>动作名称</label>
        <input data-edit="name" data-id="${action.id}" value="${esc(action.name)}" />
        <label>要点</label>
        <textarea data-edit="points" data-id="${action.id}">${esc(action.points)}</textarea>
      </td>
      <td class="edit-fields">
        <label>课上纠正</label>
        <textarea data-edit="class_note" data-id="${action.id}">${esc(action.class_note || "")}</textarea>
      </td>
      <td class="edit-fields">
        <label>组数</label>
        <input data-edit="target_sets" data-id="${action.id}" type="number" min="1" value="${action.target_sets}" />
        <label>次数，不限制就空着</label>
        <input data-edit="target_reps" data-id="${action.id}" type="number" min="1" value="${action.target_reps ?? ""}" />
        <label>秒数，不限制就空着</label>
        <input data-edit="target_seconds" data-id="${action.id}" type="number" min="1" value="${action.target_seconds ?? ""}" />
        <div class="actions">
          <button type="button" data-act="save-edit" data-id="${action.id}">保存</button>
          <button type="button" data-act="cancel-edit">取消</button>
        </div>
      </td>
    </tr>`;
  }
  return `<tr class="${esc(action.state)}">
    <td>${action.level}</td>
    <td>
      <div class="name-line"><span>${esc(action.name)}</span><button type="button" data-act="edit" data-id="${action.id}">编辑</button></div>
      <div class="field-label">要点</div>
      <div class="points">${esc(action.points)}</div>
    </td>
    <td class="class-note">${esc(action.class_note || "—")}</td>
    <td>${esc(targetText(action))}</td>
  </tr>`;
}

function renderModules() {
  return state.home.modules.map((card) => {
    const detail = state.ladders[card.id];
    const actions = detail ? detail.actions : [];
    const rows = actions.map((action) => renderActionRow(action)).join("");
    const options = actions.map((action) =>
      `<option value="${action.level}" ${action.level === card.current_level ? "selected" : ""}>第 ${action.level} 级 ${esc(action.name)}</option>`,
    ).join("");
    return `<h2 id="m-${card.id}">${card.sort_order}. ${esc(card.name)}</h2>
      <p class="purpose">${esc(card.purpose)}</p>
      <div class="level-pick">
        <label for="level-${card.id}">我现在在</label>
        <select id="level-${card.id}">${options}</select>
        <button type="button" data-act="set-level" data-module="${card.id}">设成这一级</button>
      </div>
      <div class="table-wrap"><table>
        <thead><tr><th>级</th><th>动作</th><th>课上纠正</th><th>目标</th></tr></thead>
        <tbody>${rows}</tbody>
      </table></div>`;
  }).join("");
}

function renderRecorder() {
  const workout = state.workout;
  const result = state.result;
  if (!workout && !result) {
    return `<h2>记一次训练</h2><p class="small">在上面表格的「当前」那一行点「记一次」。</p>`;
  }
  if (!workout && result) return `<h2>这次的结论</h2>${renderResult(result)}`;
  const action = workout.action;
  const sides = action.target_side === "双侧" ? ["左", "右"] : [action.target_side || "无"];
  const symptoms = ["无", "酸胀", "腰疼", "腿麻", "电流感", "刺痛", "其他"];
  return `<h2 id="recorder">正在记：${esc(action.module_name)} · Lv${action.level} ${esc(action.name)}</h2>
    <p class="small">目标：${esc(targetText(action))}。组间可以休息 30 秒。双侧动作左右各记一条，组号用同一个。</p>
    <div class="box">
      <div class="grid">
        <div><label>第几组</label><input id="set-no" type="number" min="1" value="${workout.setNo}" /></div>
        <div><label>次数</label><input id="reps" type="number" min="0" value="${workout.reps}" /></div>
        <div><label>秒数</label><input id="seconds" type="number" min="0" value="${workout.seconds}" /></div>
        <div><label>左右</label><select id="side">${sides.map((name) => `<option ${workout.side === name ? "selected" : ""}>${esc(name)}</option>`).join("")}</select></div>
        <div><label>疼痛 0-10</label><input id="pain" type="number" min="0" max="10" value="${workout.pain}" /></div>
        <div><label>症状</label><select id="symptom">${symptoms.map((name) => `<option ${workout.symptom === name ? "selected" : ""}>${esc(name)}</option>`).join("")}</select></div>
        <div><label>动作</label><select id="form">${["标准", "代偿", "无法完成"].map((name) => `<option ${workout.form === name ? "selected" : ""}>${esc(name)}</option>`).join("")}</select></div>
        <div><label>费力 1-10</label><input id="rpe" type="number" min="1" max="10" value="${workout.rpe}" /></div>
      </div>
      <div class="actions">
        <button data-act="save-set">记下这一组</button>
        <button data-act="finish">结束并看能不能升级</button>
      </div>
      <p class="small">已记 ${workout.logs.length} 条。</p>
    </div>`;
}

function renderResult(result) {
  const upgrade = result.upgrade || {};
  let conclusion = "";
  if (result.upgraded) {
    conclusion = `<div class="box ok">已到第 ${result.upgraded.current_level} 级。${esc(result.upgraded.status)}</div>`;
  } else if (upgrade.eligible) {
    const label = upgrade.module_done ? "确认完成这个模块" : "确认升到 Lv" + upgrade.next_level;
    conclusion = `<div class="box ok">达到升级标准。</div><button data-act="upgrade">${label}</button>`;
  } else {
    conclusion = `<div class="box">这次不升级。${esc((upgrade.reasons || []).join("；"))}</div>`;
  }
  const rollback = result.rollback && result.rollback.suggest
    ? `<div class="box warn">${esc(result.rollback.message)}</div>${result.rollback.can_rollback ? `<input id="rollback-reason" placeholder="回退原因，例如：腿麻，先巩固" value="${esc(state.rollbackReason)}" /><button data-act="rollback" data-module="${result.session.module_id}" data-session="${result.session.id}">回退一级</button>` : ""}`
    : "";
  return `<p>${esc(result.session.module_name)} · ${esc(result.session.action_name)} · ${result.session.total_sets} 组 · ${esc(result.session.status)}</p>
    ${result.warning ? `<div class="box warn">${esc(result.warning)}</div>` : ""}
    ${conclusion}
    ${rollback}`;
}

function renderHistory() {
  const rows = state.history.map((item) => `<tr>
    <td>${esc(item.date)}</td>
    <td>${esc(item.module_name)}</td>
    <td>Lv${item.level} ${esc(item.action_name)}</td>
    <td>${item.total_sets}</td>
    <td>${esc(item.status)}</td>
    <td>${item.symptom_flag ? "有" : ""}</td>
  </tr>`).join("");
  return `<h2>最近记录</h2>
    <div class="table-wrap"><table>
      <thead><tr><th>日期</th><th>模块</th><th>动作</th><th>组数</th><th>状态</th><th>症状</th></tr></thead>
      <tbody>${rows || `<tr><td colspan="6">还没有记录。</td></tr>`}</tbody>
    </table></div>`;
}

function renderSymptom() {
  const form = state.symptom;
  const parts = ["腰", "腿", "膝", "髋", "肩", "颈", "腕", "其他"];
  const types = ["酸胀", "腰疼", "腿麻", "电流感", "刺痛", "其他"];
  return `<h2>单独记一条症状</h2>
    <div class="grid">
      <div><label>部位</label><select id="sym-part">${parts.map((name) => `<option ${form.body_part === name ? "selected" : ""}>${esc(name)}</option>`).join("")}</select></div>
      <div><label>类型</label><select id="sym-type">${types.map((name) => `<option ${form.symptom_type === name ? "selected" : ""}>${esc(name)}</option>`).join("")}</select></div>
      <div><label>程度 0-10</label><input id="sym-severity" type="number" min="0" max="10" value="${form.severity}" /></div>
      <div><label>备注</label><input id="sym-note" value="${esc(form.note)}" /></div>
    </div>
    <div class="actions"><button data-act="save-symptom">保存症状</button></div>`;
}

function renderAdmin() {
  if (!state.adminOpen) {
    return `<h2>调整动作</h2><p class="small">康复师要改组数、次数或锁定时再打开。这台机器上的演示没有真正的登录。</p><button data-act="open-admin">打开调整表</button>`;
  }
  const tabs = state.home.modules.map((item) =>
    `<button data-act="admin-module" data-id="${item.id}">${item.id === state.adminModuleId ? "【" : ""}${esc(item.name)}${item.id === state.adminModuleId ? "】" : ""}</button>`,
  ).join(" ");
  const rows = state.adminActions.map((item) => `<tr>
    <td>${item.level}</td>
    <td><input data-edit="name" data-id="${item.id}" value="${esc(item.name)}" /></td>
    <td><input data-edit="target_sets" data-id="${item.id}" type="number" value="${item.target_sets}" /></td>
    <td><input data-edit="target_reps" data-id="${item.id}" type="number" value="${item.target_reps ?? ""}" /></td>
    <td><input data-edit="target_seconds" data-id="${item.id}" type="number" value="${item.target_seconds ?? ""}" /></td>
    <td><select data-edit="frequency" data-id="${item.id}">${["每天", "隔天", "每周2-3次", "仅康复中心", "自定义"].map((name) => `<option ${item.frequency === name ? "selected" : ""}>${esc(name)}</option>`).join("")}</select></td>
    <td><input data-edit="is_locked" data-id="${item.id}" type="checkbox" ${item.is_locked ? "checked" : ""} /></td>
    <td><button data-act="save-action" data-id="${item.id}">保存</button></td>
  </tr>`).join("");
  return `<h2>调整动作</h2>
    <p class="small">${tabs}</p>
    <div class="table-wrap"><table>
      <thead><tr><th>级</th><th>名称</th><th>组</th><th>次</th><th>秒</th><th>频率</th><th>锁定</th><th></th></tr></thead>
      <tbody>${rows}</tbody>
    </table></div>`;
}

function readWorkoutForm() {
  const workout = state.workout;
  workout.setNo = Number(document.getElementById("set-no").value) || 1;
  workout.reps = Number(document.getElementById("reps").value) || 0;
  workout.seconds = Number(document.getElementById("seconds").value) || 0;
  workout.side = document.getElementById("side").value;
  workout.pain = Number(document.getElementById("pain").value) || 0;
  workout.symptom = document.getElementById("symptom").value;
  workout.form = document.getElementById("form").value;
  workout.rpe = Number(document.getElementById("rpe").value) || 5;
}

app.addEventListener("click", async (event) => {
  const muscle = event.target.closest("[data-muscle]");
  if (muscle) {
    state.muscle = muscle.getAttribute("data-muscle");
    const y = window.scrollY;
    render();
    window.scrollTo(0, y);
    return;
  }
  const button = event.target.closest("[data-act]");
  if (!button) return;
  state.error = "";
  try {
    const act = button.dataset.act;
    if (act === "set-level") await setLevel(Number(button.dataset.module));
    if (act === "edit") {
      state.editingId = Number(button.dataset.id);
      const y = window.scrollY;
      render();
      window.scrollTo(0, y);
    }
    if (act === "cancel-edit") {
      state.editingId = 0;
      const y = window.scrollY;
      render();
      window.scrollTo(0, y);
    }
    if (act === "save-edit") await saveEdit(Number(button.dataset.id));
  } catch (error) {
    state.error = error.message;
    render();
  }
});

async function setLevel(moduleId) {
  const select = document.getElementById("level-" + moduleId);
  const level = Number(select && select.value);
  await api("/api/users/1/level", {
    method: "POST",
    body: JSON.stringify({ module_id: moduleId, level }),
  });
  state.notice = "已把这一项设到第 " + level + " 级。";
  const y = window.scrollY;
  await load();
  render();
  window.scrollTo(0, y);
}

async function startWorkout(moduleId, actionId) {
  const session = await api("/api/workout/sessions", {
    method: "POST",
    body: JSON.stringify({ user_id: 1, module_id: moduleId, action_id: actionId }),
  });
  const detail = await api("/api/actions/" + actionId + "?user_id=1");
  state.result = null;
  state.warning = "";
  state.workout = blankWorkout(session, detail.action, []);
  render();
  const node = document.getElementById("recorder");
  if (node) node.scrollIntoView({ behavior: "smooth", block: "start" });
}

async function resumeWorkout(sessionId) {
  const data = await api("/api/workout/sessions/" + sessionId);
  state.workout = blankWorkout(data.session, data.action, data.sets || []);
  state.result = null;
  render();
}

function blankWorkout(session, action, logs) {
  const side = action.target_side === "双侧" ? "左" : (action.target_side || "无");
  const maxSet = logs.reduce((max, item) => Math.max(max, item.set_no), 0);
  return {
    session,
    action,
    logs,
    setNo: maxSet + 1 || 1,
    reps: action.target_reps || 0,
    seconds: action.target_seconds || 0,
    side,
    pain: 0,
    symptom: "无",
    form: "标准",
    rpe: 5,
  };
}

async function saveSet() {
  readWorkoutForm();
  const workout = state.workout;
  const saved = await api("/api/workout/sets", {
    method: "POST",
    body: JSON.stringify({
      session_id: workout.session.id,
      set_no: workout.setNo,
      reps: workout.reps,
      seconds: workout.seconds,
      side: workout.side,
      rpe: workout.rpe,
      pain_score: workout.pain,
      symptom_type: workout.symptom,
      form_quality: workout.form,
    }),
  });
  workout.logs.push(saved.set);
  state.warning = saved.warning || "";
  if (workout.action.target_side === "双侧" && workout.side === "左") {
    workout.side = "右";
  } else {
    workout.setNo += 1;
    workout.side = workout.action.target_side === "双侧" ? "左" : workout.side;
    workout.reps = workout.action.target_reps || 0;
    workout.seconds = workout.action.target_seconds || 0;
    workout.pain = 0;
    workout.symptom = "无";
    workout.form = "标准";
  }
  render();
}

async function finishWorkout() {
  const result = await api("/api/workout/finish", {
    method: "POST",
    body: JSON.stringify({ session_id: state.workout.session.id, status: state.warning ? "中断" : "完成", note: "" }),
  });
  state.workout = null;
  state.result = result;
  state.warning = result.warning || state.warning;
  await load();
  render();
}

async function confirmUpgrade() {
  const progress = await api("/api/upgrade/confirm", {
    method: "POST",
    body: JSON.stringify({ session_id: state.result.session.id }),
  });
  state.result.upgraded = progress;
  state.notice = progress.status === "已完成"
    ? "这个模块已完成。"
    : "已升到第 " + progress.current_level + " 级。";
  await load();
  render();
}

async function rollback(moduleId, sessionId) {
  const input = document.getElementById("rollback-reason");
  const reason = (input && input.value.trim()) || state.rollbackReason.trim();
  if (!reason) {
    state.error = "请先写回退原因。";
    render();
    return;
  }
  const progress = await api("/api/rollback", {
    method: "POST",
    body: JSON.stringify({ user_id: 1, module_id: moduleId, reason, session_id: sessionId }),
  });
  state.notice = "已回退到第 " + progress.current_level + " 级。";
  state.result = null;
  await load();
  render();
}

async function saveEdit(id) {
  const current = findAction(id);
  if (!current) throw new Error("找不到这个动作");
  const read = (field) => document.querySelector(`[data-edit="${field}"][data-id="${id}"]`);
  const reps = read("target_reps").value.trim();
  const seconds = read("target_seconds").value.trim();
  const body = {
    ...current,
    name: read("name").value.trim(),
    points: read("points").value,
    class_note: read("class_note").value,
    target_sets: Number(read("target_sets").value),
    target_reps: reps === "" ? null : Number(reps),
    target_seconds: seconds === "" ? null : Number(seconds),
  };
  await api("/api/actions/" + id, {
    method: "PUT",
    body: JSON.stringify(body),
  });
  state.editingId = 0;
  state.notice = "第 " + current.level + " 级已保存。";
  const y = window.scrollY;
  await load();
  render();
  window.scrollTo(0, y);
}

load().then(render).catch((error) => {
  app.innerHTML = `<p>${esc(error.message)}</p>`;
});
