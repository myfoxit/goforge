// GoForge Admin — a dependency-free SPA against the GoForge REST API.
"use strict";

const State = {
  token: localStorage.getItem("gf_admin_token") || "",
  email: localStorage.getItem("gf_admin_email") || "",
  userId: localStorage.getItem("gf_admin_uid") || "",
  collections: [],
  info: null,
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
  State.userId = res.record.id;
  localStorage.setItem("gf_admin_token", State.token);
  localStorage.setItem("gf_admin_email", State.email);
  localStorage.setItem("gf_admin_uid", State.userId);
}
function logout() {
  State.token = ""; State.email = ""; State.userId = "";
  localStorage.removeItem("gf_admin_token");
  localStorage.removeItem("gf_admin_email");
  localStorage.removeItem("gf_admin_uid");
  location.hash = "";
  render();
}

// Change the signed-in superuser's password.
function changePassword() {
  const pw = h("input", { class: "input", type: "password", placeholder: "min 10 chars" });
  const pw2 = h("input", { class: "input", type: "password", placeholder: "confirm" });
  const body = h("div", {},
    h("div", { class: "field" }, h("label", {}, "New password"), pw),
    h("div", { class: "field" }, h("label", {}, "Confirm password"), pw2));
  modal("Change password", body, async (close) => {
    if (pw.value !== pw2.value) throw new Error("Passwords do not match.");
    await api("PATCH", "/collections/_superusers/records/" + State.userId,
      { password: pw.value, passwordConfirm: pw2.value });
    toast("Password changed", "ok"); close();
  });
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
    const [cols, info] = await Promise.all([
      api("GET", "/collections"),
      api("GET", "/settings/info").catch(() => null),
    ]);
    State.collections = cols.items || [];
    State.info = info;
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
  nav.push(navBtn("collection/_superusers", "★ Superusers"));
  if (hasModule("backups")) nav.push(navBtn("backups", "🗄 Backups"));
  nav.push(navBtn("logs", "≣ Logs"));

  nav.push(h("div", { style: "flex:1" }));
  nav.push(h("div", { class: "spread", style: "padding:8px 10px" },
    h("span", { class: "muted", style: "font-size:12.5px;overflow:hidden;text-overflow:ellipsis" }, State.email),
    h("button", { class: "btn ghost sm", title: "Toggle theme", onclick: () => { toggleTheme(); } }, "◑")));
  nav.push(h("button", { class: "nav-item", onclick: changePassword }, h("span", {}, "🔑"), "Change password"));
  nav.push(h("button", { class: "nav-item", onclick: logout }, h("span", {}, "⇦"), "Sign out"));

  return h("div", { id: "shell" },
    h("div", { id: "sidebar" }, ...nav),
    h("div", { id: "main" }));
}
function navBtn(route, label) {
  return h("button", { class: "nav-item", "data-route": route, onclick: () => go(route) }, label);
}
function go(route) { location.hash = "#/" + route; }
function hasModule(id) { return !!(State.info && (State.info.modules || []).includes(id)); }

function renderRoute() {
  const hash = location.hash.replace(/^#\/?/, "") || "dashboard";
  const [route, ...rest] = hash.split("/");
  State.route = route; State.param = rest.join("/");
  document.querySelectorAll(".nav-item").forEach((n) =>
    n.classList.toggle("active", n.getAttribute("data-route") === hash));
  const main = document.getElementById("main");
  if (!main) return;
  main.replaceChildren(h("div", { class: "muted" }, h("span", { class: "spin" }), " Loading…"));
  const view = { dashboard: viewDashboard, collection: viewCollection, settings: viewSettings, apikeys: viewApiKeys, logs: viewLogs, backups: viewBackups }[route];
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
const listState = {}; // per-collection UI state (page/sort/search/perPage)
function lsFor(name) {
  if (!listState[name]) listState[name] = { page: 1, perPage: 50, sort: "-created", search: "" };
  return listState[name];
}
// pick a human-friendly label field for relation display
function displayField(col) {
  if (!col) return "id";
  for (const p of ["name", "title", "label", "email", "slug", "username"]) {
    if (col.fields.some((f) => f.name === p)) return p;
  }
  const t = col.fields.find((f) => !f.hidden && (f.type === "text" || f.type === "email"));
  return t ? t.name : "id";
}
function searchableFields(col) {
  return col.fields.filter((f) => !f.hidden && ["text", "email", "url", "editor"].includes(f.type)).map((f) => f.name);
}
function downloadFile(name, text, type) {
  const url = URL.createObjectURL(new Blob([text], { type }));
  const a = h("a", { href: url, download: name });
  document.body.append(a); a.click(); a.remove();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}

async function viewCollection(main) {
  const name = State.param;
  const col = State.collections.find((c) => c.name === name);
  if (!col) { main.replaceChildren(h("div", { class: "empty" }, "Collection not found.")); return; }
  const isView = col.type === "view";
  const st = lsFor(name);
  let selected = new Set();
  const dataFields = col.fields.filter((f) => !f.hidden && f.type !== "autodate" && f.type !== "password");

  const countEl = h("div", { class: "muted", style: "font-size:13px" });
  const tableHost = h("div", { class: "table-wrap" });
  const pager = h("div", { class: "pager" });
  const bulkBar = h("div", { class: "row", style: "display:none;gap:8px;margin-bottom:10px" },
    h("span", { class: "muted", id: "bulk-count" }),
    h("button", { class: "btn danger sm", onclick: bulkDelete }, "Delete selected"));

  const searchI = h("input", { class: "input sm", type: "search", placeholder: "Search…", value: st.search, style: "max-width:240px" });
  let searchTimer;
  searchI.addEventListener("input", () => {
    clearTimeout(searchTimer);
    searchTimer = setTimeout(() => { st.search = searchI.value; st.page = 1; refresh(); }, 250);
  });
  const perPageSel = h("select", { class: "input sm", style: "width:auto" },
    ...[10, 25, 50, 100, 200].map((n) => { const o = h("option", { value: n }, n + " / page"); if (n === st.perPage) o.selected = true; return o; }));
  perPageSel.addEventListener("change", () => { st.perPage = Number(perPageSel.value); st.page = 1; refresh(); });

  const toolbar = h("div", { class: "toolbar" },
    h("div", {}, h("h1", { class: "page-title" }, name), countEl),
    h("div", { class: "row wrap" },
      searchableFields(col).length ? searchI : null, perPageSel,
      h("button", { class: "btn outline sm", onclick: exportJSON }, "⭳ Export"),
      !col.system && !isView && h("button", { class: "btn outline sm", onclick: () => editSchema(col) }, "⚙ Schema"),
      !col.system && !isView && h("button", { class: "btn outline sm", onclick: truncate }, "Clear"),
      !isView && h("button", { class: "btn sm", onclick: () => editRecord(col, refresh, null) }, "＋ New record")));

  main.replaceChildren(toolbar, bulkBar, tableHost, pager);
  await refresh();

  function buildFilter() {
    if (!st.search.trim()) return "";
    const fields = searchableFields(col);
    if (!fields.length) return "";
    const needle = st.search.trim().replace(/"/g, '\\"');
    return fields.map((f) => `${f} ~ "${needle}"`).join(" || ");
  }

  async function refresh() {
    selected = new Set(); updateBulk();
    tableHost.replaceChildren(h("div", { class: "muted", style: "padding:16px" }, h("span", { class: "spin" }), " Loading…"));
    let res;
    try {
      const q = new URLSearchParams({ page: String(st.page), perPage: String(st.perPage), sort: st.sort });
      const filter = buildFilter();
      if (filter) q.set("filter", filter);
      res = await api("GET", `/collections/${name}/records?` + q);
    } catch (e) { toast(e.message, "err"); res = { items: [], totalItems: 0, totalPages: 1 }; }
    const items = res.items || [];
    countEl.textContent = `${res.totalItems} records · ${col.type}`;

    const selAll = h("input", { type: "checkbox", title: "Select all" });
    selAll.addEventListener("change", () => {
      tableHost.querySelectorAll("tbody input.rowsel").forEach((cb) => {
        cb.checked = selAll.checked;
        const id = cb.getAttribute("data-id");
        if (selAll.checked) selected.add(id); else selected.delete(id);
      });
      updateBulk();
    });

    const head = h("tr", {},
      !isView && h("th", { style: "width:32px" }, selAll),
      sortHeader("id", "id"),
      ...dataFields.map((f) => sortHeader(f.name, f.name)),
      h("th", {}));
    const body = h("tbody", {});
    if (!items.length) body.append(h("tr", {}, h("td", { colspan: dataFields.length + 3, class: "empty" }, "No records.")));
    for (const r of items) body.append(recordRow(r));
    tableHost.replaceChildren(h("table", {}, h("thead", {}, head), body));
    renderPager(res);
  }

  function sortHeader(fname, label) {
    const active = st.sort === fname || st.sort === "-" + fname;
    const desc = st.sort === "-" + fname;
    return h("th", { class: "sortable" + (active ? " active" : ""), onclick: () => { st.sort = st.sort === fname ? "-" + fname : fname; refresh(); } },
      label, active ? h("span", { class: "sort-arrow" }, desc ? " ↓" : " ↑") : "");
  }

  function recordRow(r) {
    const tr = h("tr", {});
    if (!isView) {
      const cb = h("input", { type: "checkbox", class: "rowsel", "data-id": r.id });
      cb.addEventListener("click", (e) => e.stopPropagation());
      cb.addEventListener("change", () => { if (cb.checked) selected.add(r.id); else selected.delete(r.id); updateBulk(); });
      tr.append(h("td", {}, cb));
    }
    tr.append(h("td", {}, h("code", {}, String(r.id).slice(0, 10))));
    for (const f of dataFields) tr.append(h("td", {}, cellPreview(r[f.name], f)));
    tr.append(h("td", { style: "text-align:right" }, rowActions(col, r, refresh)));
    tr.style.cursor = "pointer";
    tr.addEventListener("click", () => editRecord(col, refresh, r));
    return tr;
  }

  function updateBulk() {
    bulkBar.style.display = selected.size ? "flex" : "none";
    const c = document.getElementById("bulk-count");
    if (c) c.textContent = `${selected.size} selected`;
  }
  function bulkDelete() {
    confirmModal("Delete records", `Delete ${selected.size} record(s)? This cannot be undone.`, async () => {
      for (const id of selected) await api("DELETE", `/collections/${name}/records/${id}`);
      toast("Deleted", "ok"); refresh();
    });
  }
  function truncate() {
    confirmModal("Clear all records", `Delete ALL records in "${name}"? This cannot be undone.`, async () => {
      await api("DELETE", "/collections/" + name + "/truncate");
      toast("Collection cleared", "ok"); st.page = 1; refresh();
    });
  }
  async function exportJSON() {
    const all = [];
    let page = 1;
    for (;;) {
      const q = new URLSearchParams({ page: String(page), perPage: "500", sort: st.sort });
      const r = await api("GET", `/collections/${name}/records?` + q);
      all.push(...(r.items || []));
      if (page >= (r.totalPages || 1)) break;
      page++;
    }
    downloadFile(name + ".json", JSON.stringify(all, null, 2), "application/json");
    toast(`Exported ${all.length} records`, "ok");
  }
  function renderPager(res) {
    const total = res.totalPages || 1;
    pager.replaceChildren(
      h("button", { class: "btn outline sm", disabled: st.page <= 1, onclick: () => { st.page--; refresh(); } }, "‹ Prev"),
      h("span", { class: "muted", style: "font-size:13px" }, `Page ${st.page} of ${total}`),
      h("button", { class: "btn outline sm", disabled: st.page >= total, onclick: () => { st.page++; refresh(); } }, "Next ›"));
  }
}

function cellPreview(v, f) {
  if (v == null || v === "") return h("span", { class: "muted" }, "—");
  if (typeof v === "boolean") return h("span", { class: "badge " + (v ? "green" : "") }, v ? "true" : "false");
  if (f && f.type === "file") { const arr = Array.isArray(v) ? v : [v]; return h("span", { class: "badge" }, arr.length + (arr.length === 1 ? " file" : " files")); }
  if (Array.isArray(v)) return v.length ? h("span", {}, v.join(", ").slice(0, 40)) : h("span", { class: "muted" }, "[]");
  if (typeof v === "object") return h("code", {}, JSON.stringify(v).slice(0, 40));
  const s = String(v);
  return s.length > 48 ? s.slice(0, 48) + "…" : s;
}

function rowActions(col, r, refresh) {
  return h("button", {
    class: "btn ghost sm", title: "Delete", onclick: (e) => {
      e.stopPropagation();
      confirmModal("Delete record", `Delete record ${r.id}? This cannot be undone.`, async () => {
        await api("DELETE", `/collections/${col.name}/records/${r.id}`);
        toast("Record deleted", "ok"); refresh ? refresh() : renderRoute();
      });
    }
  }, "🗑");
}

function editRecord(col, done, record) {
  const isNew = !record;
  const body = h("div", {});
  const inputs = {};
  let hasFile = false;
  for (const f of col.fields) {
    if (f.hidden && f.type !== "password") continue;
    if (f.type === "autodate") continue;
    if (f.type === "file") hasFile = true;
    const val = record ? record[f.name] : undefined;
    const { field, get } = fieldInput(f, val, isNew, col, record);
    inputs[f.name] = get;
    body.append(field);
  }
  if (col.type === "auth") {
    const pw = h("input", { class: "input", type: "password", placeholder: isNew ? "min 10 chars" : "leave blank to keep" });
    const pw2 = h("input", { class: "input", type: "password" });
    inputs["password"] = () => pw.value || undefined;
    inputs["passwordConfirm"] = () => (pw.value ? pw2.value : undefined);
    body.append(h("div", { class: "field" }, h("label", {}, "Password" + (isNew ? " *" : "")), pw),
      h("div", { class: "field" }, h("label", {}, "Confirm password"), pw2));
  }

  modal(isNew ? `New ${col.name} record` : `Edit ${col.name} record`, body, async (close) => {
    const scalar = {};
    let fileData = null;
    for (const [k, get] of Object.entries(inputs)) {
      const v = get();
      if (v === undefined) continue;
      if (v && v.__file) {
        if (v.files.length || v.remove.length) { (fileData = fileData || {})[k] = v; }
      } else {
        scalar[k] = v;
      }
    }
    if (fileData) {
      const fd = new FormData();
      for (const [k, v] of Object.entries(scalar)) fd.append(k, typeof v === "object" ? JSON.stringify(v) : String(v));
      for (const [k, v] of Object.entries(fileData)) {
        for (const file of v.files) fd.append(k, file);
        for (const rm of v.remove) fd.append(k, "-" + rm);
      }
      if (isNew) await api("POST", `/collections/${col.name}/records`, fd, true);
      else await api("PATCH", `/collections/${col.name}/records/${record.id}`, fd, true);
    } else {
      if (isNew) await api("POST", `/collections/${col.name}/records`, scalar);
      else await api("PATCH", `/collections/${col.name}/records/${record.id}`, scalar);
    }
    toast(isNew ? "Record created" : "Record updated", "ok");
    close(); done ? done() : renderRoute();
  });
}

function fieldInput(f, val, isNew, col, record) {
  const wrap = h("div", { class: "field" });
  wrap.append(h("label", {}, f.name + (f.required ? " *" : "")));
  let get = () => undefined;
  const opts = f.options || {};
  if (f.type === "bool") {
    const cb = h("input", { type: "checkbox" });
    cb.checked = !!val;
    wrap.append(h("div", { class: "row" }, cb, h("span", { class: "muted" }, "enabled")));
    get = () => cb.checked;
  } else if (f.type === "select" && opts.values) {
    const multi = (opts.maxSelect || 1) !== 1;
    const sel = h("select", { class: "input", multiple: multi, size: multi ? Math.min(opts.values.length, 5) : 1 });
    if (!multi && !f.required) sel.append(h("option", { value: "" }, "— none —"));
    for (const opt of opts.values) {
      const o = h("option", { value: opt }, opt);
      if (multi ? (val || []).includes(opt) : val === opt) o.selected = true;
      sel.append(o);
    }
    wrap.append(sel);
    get = () => multi ? Array.from(sel.selectedOptions).map((o) => o.value) : (sel.value || (f.required ? "" : undefined));
  } else if (f.type === "relation") {
    const target = opts.collection;
    const multi = (opts.maxSelect || 1) !== 1;
    const current = val == null ? [] : (Array.isArray(val) ? val : [val]);
    const sel = h("select", { class: "input", multiple: multi, size: multi ? 5 : 1 },
      h("option", { value: "" }, "Loading…"));
    (async () => {
      try {
        const tcol = State.collections.find((c) => c.name === target);
        const label = displayField(tcol);
        const r = await api("GET", `/collections/${target}/records?perPage=200&sort=${encodeURIComponent(label)}`);
        sel.replaceChildren();
        if (!multi && !f.required) sel.append(h("option", { value: "" }, "— none —"));
        for (const rec of (r.items || [])) {
          const o = h("option", { value: rec.id }, String(rec[label] ?? rec.id).slice(0, 70));
          if (current.includes(rec.id)) o.selected = true;
          sel.append(o);
        }
      } catch { sel.replaceChildren(h("option", { value: "" }, "(couldn't load " + target + ")")); }
    })();
    wrap.append(sel);
    if (target) wrap.append(h("span", { class: "muted", style: "font-size:12px" }, "→ " + target));
    get = () => {
      const vals = Array.from(sel.selectedOptions).map((o) => o.value).filter(Boolean);
      return multi ? vals : (vals[0] ?? (f.required ? "" : undefined));
    };
  } else if (f.type === "file") {
    const multi = (opts.maxSelect || 1) !== 1;
    const existing = val == null ? [] : (Array.isArray(val) ? val : [val]);
    const kept = new Set(existing);
    const list = h("div", { class: "stack", style: "gap:4px;margin-bottom:6px" });
    existing.forEach((fn) => {
      const link = record ? h("a", { href: `/api/files/${col.name}/${record.id}/${encodeURIComponent(fn)}`, target: "_blank" }, fn) : h("span", {}, fn);
      const rowEl = h("div", { class: "spread" }, link,
        h("button", { class: "btn ghost sm", type: "button", onclick: () => { kept.delete(fn); rowEl.remove(); } }, "✕"));
      list.append(rowEl);
    });
    const fileI = h("input", { type: "file", multiple: multi });
    wrap.append(list, fileI);
    get = () => ({ __file: true, files: Array.from(fileI.files || []), remove: existing.filter((fn) => !kept.has(fn)) });
  } else if (f.type === "date") {
    const inp = h("input", { class: "input", type: "datetime-local" });
    if (val) inp.value = String(val).slice(0, 16).replace(" ", "T");
    wrap.append(inp);
    get = () => { if (!inp.value) return f.required ? "" : undefined; return inp.value.replace("T", " ") + ":00"; };
  } else if (f.type === "editor" || f.type === "json") {
    const ta = h("textarea", { class: "input" }, f.type === "json" && val != null ? JSON.stringify(val, null, 2) : (val ?? ""));
    wrap.append(ta);
    get = () => {
      if (ta.value === "") return f.type === "json" ? null : "";
      if (f.type === "json") { try { return JSON.parse(ta.value); } catch { return ta.value; } }
      return ta.value;
    };
  } else {
    const type = f.type === "number" ? "number" : f.type === "email" ? "email" : f.type === "url" ? "url" : "text";
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
const FIELD_TYPES = ["text", "editor", "number", "bool", "email", "url", "date", "select", "json", "relation", "file", "password"];
function cbx(v) { const c = h("input", { type: "checkbox" }); c.checked = !!v; return c; }

// renderOptPanel fills a panel with type-specific field options and returns a
// getter producing the options object.
function renderOptPanel(panel, type, opts) {
  panel.replaceChildren();
  const inputs = {};
  function add(key, label, kind, ph) {
    const inp = h("input", { class: "input sm", type: kind === "num" ? "number" : "text", value: opts[key] ?? "", placeholder: ph || "" });
    panel.append(h("div", { class: "field", style: "margin:0 0 6px" }, h("label", { style: "font-size:12px" }, label), inp));
    inputs[key] = () => { const v = String(inp.value).trim(); if (v === "") return undefined; return kind === "num" ? Number(v) : v; };
  }
  if (type === "select") {
    const ta = h("textarea", { class: "input sm", style: "min-height:60px" }, (opts.values || []).join("\n"));
    panel.append(h("div", { class: "field", style: "margin:0 0 6px" }, h("label", { style: "font-size:12px" }, "Values (one per line)"), ta));
    inputs.values = () => ta.value.split("\n").map((s) => s.trim()).filter(Boolean);
    add("maxSelect", "Max select (1 = single)", "num", "1");
  } else if (type === "relation") {
    const sel = h("select", { class: "input sm" }, h("option", { value: "" }, "— collection —"));
    for (const c of State.collections) { const o = h("option", { value: c.name }, c.name); if (c.name === opts.collection) o.selected = true; sel.append(o); }
    panel.append(h("div", { class: "field", style: "margin:0 0 6px" }, h("label", { style: "font-size:12px" }, "Related collection"), sel));
    inputs.collection = () => sel.value || undefined;
    add("maxSelect", "Max select (1 = single)", "num", "1");
  } else if (type === "number") {
    add("min", "Min", "num"); add("max", "Max", "num");
  } else if (type === "text" || type === "editor") {
    add("min", "Min length", "num"); add("max", "Max length", "num"); add("pattern", "Regex pattern", "str");
  } else if (type === "file") {
    add("maxSelect", "Max files (1 = single)", "num", "1");
    add("maxSize", "Max size (bytes)", "num");
    add("mimeTypes", "Mime types (comma-separated)", "str", "image/png, image/jpeg");
  } else {
    panel.append(h("div", { class: "muted", style: "font-size:12px" }, "No extra options for this type."));
  }
  return () => {
    const out = {};
    for (const [k, g] of Object.entries(inputs)) { const v = g(); if (v !== undefined && !(Array.isArray(v) && !v.length)) out[k] = v; }
    if (typeof out.mimeTypes === "string") out.mimeTypes = out.mimeTypes.split(",").map((s) => s.trim()).filter(Boolean);
    return out;
  };
}

function newCollection() {
  const nameI = h("input", { class: "input", placeholder: "e.g. posts" });
  const typeI = h("select", { class: "input" }, h("option", { value: "base" }, "base"), h("option", { value: "auth" }, "auth"), h("option", { value: "view" }, "view"));
  const queryWrap = h("div", { class: "field hidden" }, h("label", {}, "View query (read-only SELECT)"),
    h("textarea", { class: "input", placeholder: "SELECT id, ... FROM ..." }));
  typeI.addEventListener("change", () => queryWrap.classList.toggle("hidden", typeI.value !== "view"));
  const body = h("div", {},
    h("div", { class: "field" }, h("label", {}, "Name"), nameI),
    h("div", { class: "field" }, h("label", {}, "Type"), typeI), queryWrap);
  modal("New collection", body, async (close) => {
    const type = typeI.value;
    const payload = { name: nameI.value.trim(), type, listRule: "", viewRule: "", fields: [] };
    if (type === "view") payload.options = { query: queryWrap.querySelector("textarea").value.trim() };
    await api("POST", "/collections", payload);
    toast("Collection created", "ok"); close();
    await render(); go("collection/" + payload.name);
  });
}

function editSchema(col) {
  const fieldRows = h("div", {});
  const idxRows = h("div", {});
  const rules = {};

  function addFieldRow(f = { name: "", type: "text", required: false, unique: false, hidden: false, options: {} }) {
    const opts = Object.assign({}, f.options || {});
    const locked = !!f.system;
    const nameI = h("input", { class: "input sm", value: f.name, placeholder: "field name", disabled: locked });
    const typeI = h("select", { class: "input sm", disabled: locked });
    for (const t of FIELD_TYPES) { const o = h("option", { value: t }, t); if (t === f.type) o.selected = true; typeI.append(o); }
    const reqI = cbx(f.required), uniI = cbx(f.unique), hidI = cbx(f.hidden);
    const optPanel = h("div", { class: "opt-panel hidden" });
    let getOpts = renderOptPanel(optPanel, typeI.value, opts);
    typeI.addEventListener("change", () => { getOpts = renderOptPanel(optPanel, typeI.value, {}); });
    const gear = h("button", { class: "btn ghost sm", type: "button", title: "Field options", onclick: () => optPanel.classList.toggle("hidden") }, "⚙");
    const del = h("button", { class: "btn ghost sm", type: "button", title: "Remove", disabled: locked, onclick: () => row.remove() }, "✕");
    const top = h("div", { class: "field-row" }, nameI, typeI,
      h("label", { class: "row", style: "font-size:12px" }, reqI, "req"),
      h("label", { class: "row", style: "font-size:12px" }, uniI, "uniq"),
      h("label", { class: "row", style: "font-size:12px" }, hidI, "hide"),
      gear, del);
    const row = h("div", { class: "schema-field" }, top, optPanel);
    row._get = () => {
      if (!nameI.value.trim()) return null;
      const out = { name: nameI.value.trim(), type: typeI.value, required: reqI.checked, unique: uniI.checked, hidden: hidI.checked };
      if (f.id) out.id = f.id;
      if (f.system) out.system = true;
      const o = getOpts();
      if (Object.keys(o).length) out.options = o;
      return out;
    };
    fieldRows.append(row);
  }
  for (const f of col.fields) addFieldRow(f);

  function addIdxRow(ix = { name: "", columns: [], unique: false }) {
    const nameI = h("input", { class: "input sm", value: ix.name, placeholder: "index name" });
    const colsI = h("input", { class: "input sm", value: (ix.columns || []).join(", "), placeholder: "col1, col2" });
    const uniI = cbx(ix.unique);
    const row = h("div", { class: "idx-row" }, nameI, colsI,
      h("label", { class: "row", style: "font-size:12px" }, uniI, "uniq"),
      h("button", { class: "btn ghost sm", type: "button", onclick: () => row.remove() }, "✕"));
    row._get = () => {
      const cols = colsI.value.split(",").map((s) => s.trim()).filter(Boolean);
      if (!nameI.value.trim() || !cols.length) return null;
      return { name: nameI.value.trim(), columns: cols, unique: uniI.checked };
    };
    idxRows.append(row);
  }
  (col.indexes || []).forEach(addIdxRow);

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
    h("h4", { style: "margin:8px 0" }, "Indexes"),
    idxRows,
    h("button", { class: "btn outline sm", onclick: () => addIdxRow(), style: "margin:4px 0 16px" }, "＋ Add index"),
    h("h4", { style: "margin:8px 0" }, "API rules"),
    h("p", { class: "muted", style: "font-size:12.5px;margin-top:0" }, "Expression like ", h("code", {}, "owner = @request.auth.id"), ". Empty = public, locked = superusers only."),
    ruleField("List", "listRule"), ruleField("View", "viewRule"),
    ruleField("Create", "createRule"), ruleField("Update", "updateRule"), ruleField("Delete", "deleteRule"));

  const m = modal(`Schema · ${col.name}`, body, async (close) => {
    const fields = Array.from(fieldRows.children).map((r) => r._get()).filter(Boolean);
    const indexes = Array.from(idxRows.children).map((r) => r._get()).filter(Boolean);
    const payload = { fields, indexes };
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

// ---- Backups ----
function fmtBytes(n) {
  n = Number(n) || 0;
  const u = ["B", "KB", "MB", "GB", "TB"];
  let i = 0;
  while (n >= 1024 && i < u.length - 1) { n /= 1024; i++; }
  return (i === 0 ? n : n.toFixed(1)) + " " + u[i];
}
async function downloadBackup(name) {
  try {
    const res = await fetch("/api/backups/" + encodeURIComponent(name), { headers: { Authorization: "Bearer " + State.token } });
    if (!res.ok) throw new Error("Download failed");
    const url = URL.createObjectURL(await res.blob());
    const a = h("a", { href: url, download: name }); document.body.append(a); a.click(); a.remove();
    setTimeout(() => URL.revokeObjectURL(url), 1000);
  } catch (e) { toast(e.message, "err"); }
}
async function viewBackups(main) {
  async function load() {
    const res = await api("GET", "/backups").catch(() => ({ items: [] }));
    const rows = (res.items || []).map((b) => ({
      name: h("code", {}, b.name),
      size: fmtBytes(b.size),
      created: b.created ? String(b.created).replace("T", " ").slice(0, 19) : "",
      _a: h("div", { class: "row", style: "justify-content:flex-end" },
        h("button", { class: "btn outline sm", onclick: () => downloadBackup(b.name) }, "⭳ Download"),
        h("button", {
          class: "btn ghost sm", onclick: () => confirmModal("Delete backup", `Delete ${b.name}?`, async () => {
            await api("DELETE", "/backups/" + encodeURIComponent(b.name)); toast("Backup deleted", "ok"); load();
          })
        }, "🗑")),
    }));
    main.replaceChildren(
      h("div", { class: "toolbar" }, h("h1", { class: "page-title" }, "Backups"),
        h("button", { class: "btn sm", onclick: create }, "＋ Create backup")),
      h("p", { class: "muted", style: "margin-top:0" }, "Snapshots of the database and uploaded files (.tar.gz)."),
      h("div", { class: "table-wrap" }, table(
        [{ key: "name", label: "Name" }, { key: "size", label: "Size", align: "right" },
        { key: "created", label: "Created" }, { key: "_a", label: "", align: "right" }], rows)));
  }
  async function create() {
    toast("Creating backup…");
    try { const r = await api("POST", "/backups"); toast("Backup created: " + r.name, "ok"); load(); }
    catch (e) { toast(e.message, "err"); }
  }
  await load();
}

// ---- boot ----
initTheme();
window.addEventListener("hashchange", renderRoute);
render();
