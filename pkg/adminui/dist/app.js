// GoForge Admin — a dependency-free SPA against the GoForge REST API.
"use strict";

const State = {
  token: localStorage.getItem("gf_admin_token") || "",
  email: localStorage.getItem("gf_admin_email") || "",
  collections: [],
  route: "dashboard",
  param: "",
};

// ---- API ----
async function api(method, path, body, isForm) {
  const headers = {};
  if (State.token) headers["Authorization"] = "Bearer " + State.token;
  let payload;
  if (isForm) {
    payload = body;
  } else if (body !== undefined) {
    headers["Content-Type"] = "application/json";
    payload = JSON.stringify(body);
  }
  const res = await fetch("/api" + path, { method, headers, body: payload });
  if (res.status === 204) return null;
  const text = await res.text();
  let data = null;
  try { data = text ? JSON.parse(text) : null; } catch { data = text; }
  if (!res.ok) {
    const msg = (data && data.message) || res.statusText;
    const err = new Error(msg);
    err.data = data && data.data;
    err.status = res.status;
    throw err;
  }
  return data;
}

// ---- utils ----
const h = (tag, attrs = {}, ...kids) => {
  const el = document.createElement(tag);
  for (const [k, v] of Object.entries(attrs)) {
    if (k === "class") el.className = v;
    else if (k === "html") el.innerHTML = v;
    else if (k.startsWith("on")) el.addEventListener(k.slice(2), v);
    else if (v === true) el.setAttribute(k, "");
    else if (v !== false && v != null) el.setAttribute(k, v);
  }
  for (const kid of kids.flat()) {
    if (kid == null || kid === false) continue;
    el.appendChild(typeof kid === "string" ? document.createTextNode(kid) : kid);
  }
  return el;
};
const esc = (s) => String(s ?? "").replace(/[&<>"]/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[c]));

function toast(msg, kind = "") {
  const t = h("div", { class: "toast " + kind }, msg);
  document.getElementById("toasts").appendChild(t);
  setTimeout(() => t.remove(), 4200);
}

function modal(title, contentNode, onSave, saveLabel = "Save") {
  const overlay = h("div", { class: "overlay", onclick: close });
  const footer = h("div", { class: "modal-footer" },
    h("button", { class: "btn outline", onclick: close }, "Cancel"),
    onSave && h("button", { class: "btn", onclick: doSave }, saveLabel));
  const box = h("div", { class: "modal" }, h("h3", {}, title), contentNode, footer);
  function close() { overlay.remove(); box.remove(); }
  async function doSave() {
    try { await onSave(close); } catch (e) { toast(e.message, "err"); }
  }
  document.body.append(overlay, box);
  const first = box.querySelector("input,select,textarea");
  if (first) first.focus();
  return { close };
}

function confirmModal(title, message, onConfirm) {
  const body = h("p", { class: "muted" }, message);
  modal(title, body, async (close) => { await onConfirm(); close(); }, "Confirm");
}

// ---- auth ----
async function login(email, password) {
  const res = await api("POST", "/collections/_superusers/auth-with-password", { identity: email, password });
  State.token = res.token;
  State.email = res.record.email;
  localStorage.setItem("gf_admin_token", State.token);
  localStorage.setItem("gf_admin_email", State.email);
}
function logout() {
  State.token = ""; State.email = "";
  localStorage.removeItem("gf_admin_token");
  localStorage.removeItem("gf_admin_email");
  location.hash = "";
  render();
}

// ---- theme ----
function initTheme() {
  const saved = localStorage.getItem("gf_theme");
  const dark = saved ? saved === "dark" : matchMedia("(prefers-color-scheme: dark)").matches;
  document.documentElement.classList.toggle("dark", dark);
}
function toggleTheme() {
  const dark = !document.documentElement.classList.contains("dark");
  document.documentElement.classList.toggle("dark", dark);
  localStorage.setItem("gf_theme", dark ? "dark" : "light");
}

// ---- render root ----
async function render() {
  const app = document.getElementById("app");
  if (!State.token) { app.replaceChildren(loginView()); return; }
  try {
    const res = await api("GET", "/collections");
    State.collections = res.items || [];
  } catch (e) {
    if (e.status === 401) { logout(); return; }
    toast(e.message, "err");
  }
  app.replaceChildren(shellView());
  renderRoute();
}

function loginView() {
  const email = h("input", { class: "input", type: "email", placeholder: "you@example.com", value: State.email });
  const pass = h("input", { class: "input", type: "password", placeholder: "password" });
  const err = h("div", { class: "muted", style: "color:hsl(var(--danger));font-size:13px;min-height:18px" });
  async function submit() {
    err.textContent = "";
    try { await login(email.value.trim(), pass.value); render(); }
    catch (e) { err.textContent = e.message; }
  }
  const onKey = (e) => e.key === "Enter" && submit();
  email.addEventListener("keydown", onKey); pass.addEventListener("keydown", onKey);
  return h("div", { id: "login" },
    h("div", { class: "card" },
      h("div", { class: "row", style: "font-size:20px;font-weight:700;margin-bottom:6px" }, "⚒ GoForge"),
      h("p", { class: "muted", style: "margin-top:0" }, "Sign in to the admin dashboard."),
      h("div", { class: "field" }, h("label", {}, "Email"), email),
      h("div", { class: "field" }, h("label", {}, "Password"), pass),
      err,
      h("button", { class: "btn", style: "width:100%;margin-top:6px", onclick: submit }, "Sign in"),
      h("p", { class: "muted", style: "font-size:12.5px;margin-bottom:0" },
        "No superuser yet? Run ", h("code", {}, "app superuser create <email> <password>"))));
}

const NAV = [
  { id: "dashboard", label: "Dashboard", icon: "▤" },
  { id: "settings", label: "Settings", icon: "⚙" },
  { id: "apikeys", label: "API Keys & MCP", icon: "🔑" },
  { id: "logs", label: "Logs", icon: "≣" },
];

function shellView() {
  const nav = [];
  nav.push(h("div", { class: "brand" }, "⚒ GoForge"));
  nav.push(h("button", { class: "nav-item", "data-route": "dashboard", onclick: () => go("dashboard") }, h("span", {}, "▤"), "Dashboard"));

  nav.push(h("div", { class: "nav-head" }, "Collections"));
  for (const c of State.collections) {
    if (c.system && c.name.startsWith("_")) continue;
    nav.push(navBtn("collection/" + c.name, (c.type === "view" ? "◫ " : "▦ ") + c.name));
  }
  nav.push(h("button", { class: "nav-item", style: "color:hsl(var(--muted))", onclick: newCollection }, h("span", {}, "＋"), "New collection"));

  nav.push(h("div", { class: "nav-head" }, "System"));
  for (const c of State.collections) {
    if (c.system && c.name.startsWith("_") && c.type !== "view") nav.push(navBtn("collection/" + c.name, "⚙ " + c.name));
  }

  nav.push(h("div", { class: "nav-head" }, "Manage"));
  nav.push(navBtn("settings", "⚙ Settings"));
  nav.push(navBtn("apikeys", "🔑 API Keys & MCP"));
  nav.push(navBtn("logs", "≣ Logs"));

  nav.push(h("div", { style: "flex:1" }));
  nav.push(h("div", { class: "spread", style: "padding:8px 10px" },
    h("span", { class: "muted", style: "font-size:12.5px;overflow:hidden;text-overflow:ellipsis" }, State.email),
    h("button", { class: "btn ghost sm", title: "Toggle theme", onclick: () => { toggleTheme(); } }, "◑")));
  nav.push(h("button", { class: "nav-item", onclick: logout }, h("span", {}, "⇦"), "Sign out"));

  return h("div", { id: "shell" },
    h("div", { id: "sidebar" }, ...nav),
    h("div", { id: "main" }));
}
function navBtn(route, label) {
  return h("button", { class: "nav-item", "data-route": route, onclick: () => go(route) }, label);
}
function go(route) { location.hash = "#/" + route; }

function renderRoute() {
  const hash = location.hash.replace(/^#\/?/, "") || "dashboard";
  const [route, ...rest] = hash.split("/");
  State.route = route; State.param = rest.join("/");
  document.querySelectorAll(".nav-item").forEach((n) =>
    n.classList.toggle("active", n.getAttribute("data-route") === hash));
  const main = document.getElementById("main");
  if (!main) return;
  main.replaceChildren(h("div", { class: "muted" }, h("span", { class: "spin" }), " Loading…"));
  const view = { dashboard: viewDashboard, collection: viewCollection, settings: viewSettings, apikeys: viewApiKeys, logs: viewLogs }[route];
  (view || viewDashboard)(main).catch((e) => {
    main.replaceChildren(h("div", { class: "empty" }, e.message));
    if (e.status === 401) logout();
  });
}

// ---- Dashboard ----
async function viewDashboard(main) {
  const info = await api("GET", "/settings/info");
  const stats = [
    h("div", { class: "stat" }, h("div", { class: "n" }, String(State.collections.length)), h("div", { class: "l" }, "Collections")),
    h("div", { class: "stat" }, h("div", { class: "n" }, info.dbDriver), h("div", { class: "l" }, "Database")),
    h("div", { class: "stat" }, h("div", { class: "n" }, info.version), h("div", { class: "l" }, "Version")),
    h("div", { class: "stat" }, h("div", { class: "n" }, String((info.modules || []).length)), h("div", { class: "l" }, "Modules")),
  ];
  const counts = Object.entries(info.counts || {}).filter(([k]) => !k.startsWith("_"));
  main.replaceChildren(
    h("div", { class: "toolbar" }, h("h1", { class: "page-title" }, "Dashboard")),
    h("div", { class: "grid2", style: "margin-bottom:20px" }, ...stats),
    h("h3", {}, "Records"),
    counts.length ? h("div", { class: "table-wrap" }, table(
      [{ key: "c", label: "Collection" }, { key: "n", label: "Records", align: "right" }],
      counts.map(([k, v]) => ({ c: k, n: String(v) })))) : h("p", { class: "muted" }, "No user collections yet."),
    h("p", { class: "muted", style: "margin-top:20px" }, "Modules: ", ...(info.modules || []).map((m) => h("span", { class: "badge", style: "margin-right:4px" }, m))));
}

function table(cols, rows, onRow) {
  const thead = h("thead", {}, h("tr", {}, ...cols.map((c) => h("th", { style: c.align ? "text-align:" + c.align : "" }, c.label))));
  const tbody = h("tbody", {});
  if (!rows.length) tbody.append(h("tr", {}, h("td", { colspan: cols.length, class: "empty" }, "No records.")));
  for (const r of rows) {
    const tr = h("tr", onRow ? { style: "cursor:pointer", onclick: () => onRow(r) } : {},
      ...cols.map((c) => {
        const v = c.render ? c.render(r) : r[c.key];
        return h("td", { style: c.align ? "text-align:" + c.align : "" }, typeof v === "string" || typeof v === "number" ? String(v) : (v || ""));
      }));
    tbody.append(tr);
  }
  return h("table", {}, thead, tbody);
}

// ---- Collection records ----
async function viewCollection(main) {
  const name = State.param;
  const col = State.collections.find((c) => c.name === name);
  if (!col) { main.replaceChildren(h("div", { class: "empty" }, "Collection not found.")); return; }
  const isView = col.type === "view";

  const q = new URLSearchParams({ perPage: "50", sort: "-created" });
  const res = await api("GET", `/collections/${name}/records?` + q);
  const items = res.items || [];

  const visibleFields = col.fields.filter((f) => !f.hidden).slice(0, 6);
  const cols = [{ key: "id", label: "id", render: (r) => h("code", {}, r.id.slice(0, 8)) }];
  for (const f of visibleFields) cols.push({ key: f.name, label: f.name, render: (r) => cellPreview(r[f.name]) });
  cols.push({ key: "_a", label: "", align: "right", render: (r) => rowActions(col, r) });

  const toolbar = h("div", { class: "toolbar" },
    h("div", {}, h("h1", { class: "page-title" }, name),
      h("div", { class: "muted", style: "font-size:13px" }, `${res.totalItems} records · ${col.type}`)),
    h("div", { class: "row" },
      !col.system && !isView && h("button", { class: "btn outline sm", onclick: () => editSchema(col) }, "⚙ Schema"),
      !isView && h("button", { class: "btn sm", onclick: () => editRecord(col, null) }, "＋ New record")));

  main.replaceChildren(toolbar, h("div", { class: "table-wrap" }, table(cols, items, (r) => editRecord(col, r))));
}

function cellPreview(v) {
  if (v == null) return h("span", { class: "muted" }, "—");
  if (typeof v === "boolean") return h("span", { class: "badge " + (v ? "green" : "") }, v ? "true" : "false");
  if (Array.isArray(v)) return v.length ? v.join(", ").slice(0, 40) : h("span", { class: "muted" }, "[]");
  if (typeof v === "object") return h("code", {}, JSON.stringify(v).slice(0, 40));
  const s = String(v);
  return s.length > 48 ? s.slice(0, 48) + "…" : s;
}

function rowActions(col, r) {
  return h("div", { class: "row", style: "justify-content:flex-end" },
    h("button", {
      class: "btn ghost sm", onclick: (e) => {
        e.stopPropagation();
        confirmModal("Delete record", `Delete record ${r.id}? This cannot be undone.`, async () => {
          await api("DELETE", `/collections/${col.name}/records/${r.id}`);
          toast("Record deleted", "ok"); renderRoute();
        });
      }
    }, "🗑"));
}

function editRecord(col, record) {
  const isNew = !record;
  const body = h("div", {});
  const inputs = {};
  for (const f of col.fields) {
    if (f.hidden && f.type !== "password") continue;
    if (f.type === "autodate") continue;
    const val = record ? record[f.name] : undefined;
    const { field, get } = fieldInput(f, val, isNew);
    inputs[f.name] = get;
    body.append(field);
  }
  if (col.type === "auth" && isNew) {
    const pw = h("input", { class: "input", type: "password", placeholder: "min 10 chars" });
    const pw2 = h("input", { class: "input", type: "password" });
    inputs["password"] = () => pw.value;
    inputs["passwordConfirm"] = () => pw2.value;
    body.append(h("div", { class: "field" }, h("label", {}, "Password"), pw),
      h("div", { class: "field" }, h("label", {}, "Confirm password"), pw2));
  }

  modal(isNew ? `New ${col.name} record` : `Edit ${col.name} record`, body, async (close) => {
    const data = {};
    for (const [k, get] of Object.entries(inputs)) {
      const v = get();
      if (v !== undefined) data[k] = v;
    }
    if (isNew) await api("POST", `/collections/${col.name}/records`, data);
    else await api("PATCH", `/collections/${col.name}/records/${record.id}`, data);
    toast(isNew ? "Record created" : "Record updated", "ok");
    close(); renderRoute();
  });
}

function fieldInput(f, val, isNew) {
  const wrap = h("div", { class: "field" });
  wrap.append(h("label", {}, f.name + (f.required ? " *" : "")));
  let get = () => undefined;
  if (f.type === "bool") {
    const cb = h("input", { type: "checkbox" });
    cb.checked = !!val;
    wrap.append(h("div", { class: "row" }, cb, h("span", { class: "muted" }, "enabled")));
    get = () => cb.checked;
  } else if (f.type === "select" && f.options && f.options.values) {
    const multi = (f.options.maxSelect || 1) > 1;
    const sel = h("select", { class: "input", multiple: multi });
    for (const opt of f.options.values) {
      const o = h("option", { value: opt }, opt);
      if (multi ? (val || []).includes(opt) : val === opt) o.selected = true;
      sel.append(o);
    }
    wrap.append(sel);
    get = () => multi ? Array.from(sel.selectedOptions).map((o) => o.value) : sel.value;
  } else if (f.type === "editor" || f.type === "json") {
    const ta = h("textarea", { class: "input" }, f.type === "json" && val != null ? JSON.stringify(val, null, 2) : (val ?? ""));
    wrap.append(ta);
    get = () => {
      if (ta.value === "") return f.type === "json" ? null : "";
      if (f.type === "json") { try { return JSON.parse(ta.value); } catch { return ta.value; } }
      return ta.value;
    };
  } else {
    const type = f.type === "number" ? "number" : f.type === "date" ? "text" : "text";
    const inp = h("input", { class: "input", type, value: val ?? "", placeholder: f.type });
    wrap.append(inp);
    get = () => {
      if (inp.value === "") return f.required ? "" : undefined;
      return f.type === "number" ? Number(inp.value) : inp.value;
    };
  }
  return { field: wrap, get };
}

// ---- Schema editor ----
function newCollection() {
  const nameI = h("input", { class: "input", placeholder: "e.g. posts" });
  const typeI = h("select", { class: "input" }, h("option", { value: "base" }, "base"), h("option", { value: "auth" }, "auth"), h("option", { value: "view" }, "view"));
  const body = h("div", {},
    h("div", { class: "field" }, h("label", {}, "Name"), nameI),
    h("div", { class: "field" }, h("label", {}, "Type"), typeI));
  modal("New collection", body, async (close) => {
    const payload = { name: nameI.value.trim(), type: typeI.value, listRule: "", viewRule: "", fields: [] };
    if (typeI.value === "view") payload.options = { query: "SELECT id FROM ..." };
    await api("POST", "/collections", payload);
    toast("Collection created", "ok"); close();
    await render(); go("collection/" + payload.name);
  });
}

function editSchema(col) {
  const fieldRows = h("div", {});
  const rules = {};
  function addFieldRow(f = { name: "", type: "text", required: false, unique: false }) {
    const nameI = h("input", { class: "input sm", value: f.name, placeholder: "field name" });
    const typeI = h("select", { class: "input sm" });
    for (const t of ["text", "editor", "number", "bool", "email", "url", "date", "select", "json", "relation", "file", "password"]) {
      const o = h("option", { value: t }, t); if (t === f.type) o.selected = true; typeI.append(o);
    }
    const reqI = h("input", { type: "checkbox" }); reqI.checked = !!f.required;
    const uniI = h("input", { type: "checkbox" }); uniI.checked = !!f.unique;
    const opts = f.options || {};
    const row = h("div", { class: "field-row" }, nameI, typeI,
      h("label", { class: "row", style: "font-size:12px" }, reqI, "req"),
      h("label", { class: "row", style: "font-size:12px" }, uniI, "uniq"),
      h("button", { class: "btn ghost sm", onclick: () => row.remove() }, "✕"));
    row._get = () => {
      if (!nameI.value.trim()) return null;
      const out = { name: nameI.value.trim(), type: typeI.value, required: reqI.checked, unique: uniI.checked };
      if (f.id) out.id = f.id;
      if (f.system) { out.system = true; }
      if (typeI.value === "select") out.options = { values: (opts.values || ["a", "b"]), maxSelect: opts.maxSelect || 1 };
      if (typeI.value === "relation") out.options = { collection: opts.collection || "", maxSelect: opts.maxSelect || 1 };
      if (typeI.value === "file") out.options = { maxSelect: opts.maxSelect || 1 };
      return out;
    };
    fieldRows.append(row);
  }
  for (const f of col.fields) addFieldRow(f);

  function ruleField(label, key) {
    const cur = col[key];
    const inp = h("input", { class: "input", value: cur == null ? "" : cur, placeholder: cur === null ? "locked (superusers only)" : "public — any request" });
    const lock = h("input", { type: "checkbox" }); lock.checked = cur === null;
    lock.addEventListener("change", () => { inp.disabled = lock.checked; });
    inp.disabled = lock.checked;
    rules[key] = () => lock.checked ? null : inp.value;
    return h("div", { class: "field" },
      h("label", { class: "spread" }, label, h("label", { class: "row muted", style: "font-size:12px;font-weight:400" }, lock, "lock")), inp);
  }

  const body = h("div", {},
    h("h4", { style: "margin:0 0 8px" }, "Fields"),
    fieldRows,
    h("button", { class: "btn outline sm", onclick: () => addFieldRow(), style: "margin:4px 0 16px" }, "＋ Add field"),
    h("h4", { style: "margin:8px 0" }, "API rules"),
    h("p", { class: "muted", style: "font-size:12.5px;margin-top:0" }, "Expression like ", h("code", {}, "owner = @request.auth.id"), ". Empty = public, locked = superusers only."),
    ruleField("List", "listRule"), ruleField("View", "viewRule"),
    ruleField("Create", "createRule"), ruleField("Update", "updateRule"), ruleField("Delete", "deleteRule"));

  const m = modal(`Schema · ${col.name}`, body, async (close) => {
    const fields = Array.from(fieldRows.children).map((r) => r._get()).filter(Boolean);
    const payload = { fields };
    for (const [k, get] of Object.entries(rules)) payload[k] = get();
    await api("PATCH", "/collections/" + col.name, payload);
    toast("Schema saved", "ok"); close(); await render(); go("collection/" + col.name);
  }, "Save schema");

  // Danger zone: delete collection
  if (!col.system) {
    const footer = m ? document.querySelector(".modal .modal-footer") : null;
    if (footer) footer.prepend(h("button", {
      class: "btn danger", style: "margin-right:auto", onclick: () => {
        confirmModal("Delete collection", `Delete "${col.name}" and all its data?`, async () => {
          await api("DELETE", "/collections/" + col.name);
          toast("Collection deleted", "ok"); m.close(); await render(); go("dashboard");
        });
      }
    }, "Delete collection"));
  }
}

// ---- Settings ----
async function viewSettings(main) {
  const res = await api("GET", "/settings");
  const sections = res.sections || [];
  const getters = {};
  const cards = sections.map((sec) => {
    const fields = sec.fields.map((f) => {
      const wrap = h("div", { class: "field" });
      wrap.append(h("label", {}, f.label));
      let get;
      if (f.type === "bool") {
        const cb = h("input", { type: "checkbox" }); cb.checked = !!f.value;
        wrap.append(h("div", { class: "row" }, cb, h("span", { class: "muted" }, f.help || "")));
        get = () => cb.checked;
      } else if (f.type === "select") {
        const sel = h("select", { class: "input" });
        (f.options || []).forEach((o) => { const opt = h("option", { value: o }, o); if (o === f.value) opt.selected = true; sel.append(opt); });
        wrap.append(sel); if (f.help) wrap.append(h("span", { class: "muted", style: "font-size:12px" }, f.help));
        get = () => sel.value;
      } else if (f.type === "textarea") {
        const ta = h("textarea", { class: "input" }, f.value || ""); wrap.append(ta); get = () => ta.value;
      } else {
        const inp = h("input", { class: "input", type: f.type === "secret" ? "password" : "text", value: f.value ?? "", placeholder: f.placeholder || "" });
        wrap.append(inp); if (f.help) wrap.append(h("span", { class: "muted", style: "font-size:12px" }, f.help));
        get = () => inp.value;
      }
      getters[f.key] = { get, orig: f.value, type: f.type };
      return wrap;
    });
    return h("div", { class: "card", style: "padding:18px;margin-bottom:16px" }, h("h3", { style: "margin-top:0" }, sec.title), ...fields);
  });

  async function save() {
    const patch = {};
    for (const [k, g] of Object.entries(getters)) {
      const v = g.get();
      if (g.type === "secret" && v === "••••••") continue;
      if (v !== g.orig) patch[k] = v;
    }
    if (!Object.keys(patch).length) { toast("Nothing changed"); return; }
    await api("PATCH", "/settings", patch);
    toast("Settings saved", "ok"); renderRoute();
  }
  async function testEmail() {
    const to = prompt("Send a test email to:");
    if (!to) return;
    try { await api("POST", "/settings/test-email", { to }); toast("Test email sent (or logged)", "ok"); }
    catch (e) { toast(e.message, "err"); }
  }

  main.replaceChildren(
    h("div", { class: "toolbar" }, h("h1", { class: "page-title" }, "Settings"),
      h("div", { class: "row" },
        h("button", { class: "btn outline sm", onclick: testEmail }, "✉ Test email"),
        h("button", { class: "btn sm", onclick: save }, "Save changes"))),
    ...cards);
}

// ---- API keys & MCP ----
async function viewApiKeys(main) {
  const [keys, mcp] = await Promise.all([api("GET", "/apikeys"), api("GET", "/mcp/info").catch(() => null)]);
  const rows = (keys.items || []).map((k) => ({
    name: k.name,
    tail: h("code", {}, "…" + k.keyTail),
    scopes: (k.scopes || []).join(", ") || "*",
    admin: k.admin ? h("span", { class: "badge amber" }, "admin") : "",
    used: k.lastUsed || "never",
    _a: h("button", {
      class: "btn ghost sm", onclick: () => confirmModal("Revoke key", `Revoke "${k.name}"?`, async () => {
        await api("DELETE", "/apikeys/" + k.id); toast("Key revoked", "ok"); renderRoute();
      })
    }, "Revoke"),
  }));

  const mcpCard = mcp ? h("div", { class: "card", style: "padding:18px;margin-bottom:18px" },
    h("h3", { style: "margin-top:0" }, "Connect to an AI (MCP)"),
    h("p", { class: "muted", style: "margin-top:0" }, "This app is an MCP server. Create an API key above, then connect it:"),
    codeBlock(mcp.snippets.claudeCode),
    h("p", { class: "muted", style: "font-size:12.5px" }, "Endpoint: ", h("code", {}, mcp.endpoint), " · admin keys can build the schema; scoped keys are limited to their collections.")) : null;

  main.replaceChildren(
    h("div", { class: "toolbar" }, h("h1", { class: "page-title" }, "API Keys & MCP"),
      h("button", { class: "btn sm", onclick: newApiKey }, "＋ New key")),
    mcpCard,
    h("div", { class: "table-wrap" }, table(
      [{ key: "name", label: "Name" }, { key: "tail", label: "Key" }, { key: "scopes", label: "Scopes" },
      { key: "admin", label: "" }, { key: "used", label: "Last used" }, { key: "_a", label: "", align: "right" }], rows)));
}

function codeBlock(text) {
  const pre = h("pre", {}, text);
  const btn = h("button", { class: "btn outline sm", style: "position:absolute;top:8px;right:8px", onclick: () => { navigator.clipboard.writeText(text); toast("Copied", "ok"); } }, "Copy");
  pre.append(btn);
  return pre;
}

function newApiKey() {
  const nameI = h("input", { class: "input", placeholder: "e.g. Claude Desktop" });
  const adminI = h("input", { type: "checkbox" }); adminI.checked = true;
  const scopesI = h("input", { class: "input", placeholder: "* or posts:read,posts:create (blank = all)" });
  const body = h("div", {},
    h("div", { class: "field" }, h("label", {}, "Name"), nameI),
    h("div", { class: "field" }, h("label", {}, "Scopes"), scopesI,
      h("span", { class: "muted", style: "font-size:12px" }, "collection:action pairs; blank grants everything")),
    h("div", { class: "field" }, h("label", { class: "row" }, adminI, "Admin key (schema + settings tools, bypasses rules)")));
  modal("New API key", body, async (close) => {
    const scopes = scopesI.value.trim() ? scopesI.value.split(",").map((s) => s.trim()).filter(Boolean) : [];
    const res = await api("POST", "/apikeys", { name: nameI.value.trim(), admin: adminI.checked, scopes });
    close();
    modal("API key created", h("div", {},
      h("p", { class: "muted" }, "Copy this key now — it won't be shown again:"),
      codeBlock(res.key)), null);
    renderRoute();
  }, "Create key");
}

// ---- Logs ----
async function viewLogs(main) {
  const logsCol = State.collections.find((c) => c.name === "_logs");
  if (!logsCol) { main.replaceChildren(h("div", { class: "empty" }, "The logs module is not enabled.")); return; }
  const res = await api("GET", "/collections/_logs/records?perPage=100&sort=-created");
  const rows = (res.items || []).map((r) => ({
    when: r.created,
    method: h("span", { class: "badge" }, r.method),
    path: r.path,
    status: h("span", { class: "badge " + (r.status < 300 ? "green" : r.status < 500 ? "amber" : "red") }, String(r.status)),
    ms: (r.durationMs ?? 0) + "ms",
    ip: r.ip || "",
  }));
  main.replaceChildren(
    h("div", { class: "toolbar" }, h("h1", { class: "page-title" }, "Request logs"),
      h("button", { class: "btn outline sm", onclick: renderRoute }, "↻ Refresh")),
    h("div", { class: "table-wrap" }, table(
      [{ key: "when", label: "Time" }, { key: "method", label: "Method" }, { key: "path", label: "Path" },
      { key: "status", label: "Status" }, { key: "ms", label: "Duration", align: "right" }, { key: "ip", label: "IP" }], rows)));
}

// ---- boot ----
initTheme();
window.addEventListener("hashchange", renderRoute);
render();
