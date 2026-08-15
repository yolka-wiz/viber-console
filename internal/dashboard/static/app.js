/* viber-console SPA — vanilla JS, no dependencies */
"use strict";

const POLL_MS = 10000;
const state = {
  overview: null,
  schema: null,
  values: null,
  configs: null,
  group: "viberayd",
  configsTimer: null,
};

const $ = (sel) => document.querySelector(sel);

/* ---------- helpers ---------- */

async function api(path, opts) {
  const res = await fetch(path, opts);
  if (!res.ok) {
    let msg = res.statusText;
    try {
      const j = await res.json();
      if (j.error) msg = j.error;
    } catch (_) {}
    throw new Error(msg || `HTTP ${res.status}`);
  }
  return res.json();
}

function fmt(n) {
  if (n == null || isNaN(n)) return "–";
  return new Intl.NumberFormat().format(n);
}

function fmtMs(s) {
  if (s == null || isNaN(s) || s <= 0) return "–";
  return s >= 1 ? s.toFixed(2) + "s" : Math.round(s * 1000) + "ms";
}

function stateClass(st) {
  if (st === "working") return "ok";
  if (st === "unreachable") return "err";
  return "warn";
}

function esc(s) {
  return String(s ?? "").replace(/[&<>"']/g, (c) =>
    ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
}

let toastTimer = null;
function toast(msg, kind) {
  const el = $("#toast");
  el.textContent = msg;
  el.className = "toast " + (kind || "");
  el.hidden = false;
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => (el.hidden = true), 3500);
}

/* ---------- theme ---------- */

function applyTheme(theme) {
  document.documentElement.dataset.theme = theme;
  $("#theme-toggle").textContent = theme === "dark" ? "🌙" : "☀️";
}

function initTheme() {
  const saved = localStorage.getItem("viber-theme");
  const preferred = saved || (window.matchMedia("(prefers-color-scheme: light)").matches ? "light" : "dark");
  applyTheme(preferred);
  $("#theme-toggle").addEventListener("click", () => {
    const next = document.documentElement.dataset.theme === "dark" ? "light" : "dark";
    localStorage.setItem("viber-theme", next);
    applyTheme(next);
  });
}

/* ---------- overview ---------- */

async function loadOverview() {
  try {
    const o = await api("/api/overview");
    state.overview = o;

    const vd = o.viberayd || {};
    const vx = o.viberoxy || {};

    $("#stat-working").textContent = fmt(vd.stats?.working);
    $("#stat-failed").textContent = fmt(vd.stats?.failed);
    $("#stat-wans").textContent = fmt(vx.wans?.active);
    $("#stat-p50").textContent = fmtMs(vx.proxy?.latency_p50_s);

    setPill("#pill-viberayd", vd.reachable, vd.reachable ? "● viberayd up" : "● viberayd down");
    setPill("#pill-viberoxy", vx.reachable, vx.reachable ? "● viberoxy up" : "● viberoxy down");

    renderDaemonStatus(vd, vx);
    renderWANs(vx);
    $("#last-updated").textContent = "updated " + new Date(o.generated_at).toLocaleTimeString();
  } catch (e) {
    toast("overview failed: " + e.message, "err");
  }
}

function setPill(sel, ok, text) {
  const el = $(sel);
  el.textContent = text;
  el.className = "pill " + (ok ? "ok" : "err");
}

function renderDaemonStatus(vd, vx) {
  const el = $("#daemon-status");
  const vdCard = `
    <div class="card">
      <strong>viberayd</strong>
      <div class="muted">${vd.reachable ? "reachable" : "unreachable"}</div>
      ${vd.stats ? `<div class="muted">${fmt(vd.stats.working)} working / ${fmt(vd.stats.total)} total</div>` : ""}
      ${vd.sub_url ? `<div class="muted">sub: ${esc(vd.sub_url)}</div>` : ""}
    </div>`;
  const vxCard = `
    <div class="card">
      <strong>viberoxy</strong>
      <div class="muted">${vx.reachable ? "reachable" : "unreachable"}</div>
      ${vx.health ? `<div class="muted">healthz: ${esc(vx.health.healthz)} · readyz: ${esc(vx.health.readyz)}</div>` : ""}
      ${vx.proxy ? `<div class="muted">${fmt(vx.proxy.connections_total)} conns · up ${fmt(vx.proxy.bytes_up)}B · down ${fmt(vx.proxy.bytes_down)}B</div>` : ""}
    </div>`;
  el.innerHTML = vdCard + vxCard;
}

/* ---------- WANs ---------- */

function renderWANs(vx) {
  const el = $("#wan-grid");
  const slots = vx.wans?.slots || [];
  if (!slots.length) {
    el.innerHTML = `<div class="empty-state">No WAN slots${vx.reachable ? "" : " (viberoxy unreachable)"}</div>`;
    return;
  }
  el.innerHTML = slots.map((s) => {
    const protoTags = Object.entries(s.proto_conns || {})
      .map(([p, c]) => `<span class="tag">${esc(p)}:${c}</span>`).join(" ");
    const stab = s.stability > 0 ? `<span class="tag">stability ${s.stability}</span>` : "";
    return `
      <div class="card wan-card">
        <div class="wan-name"><span>WAN ${s.index}</span>${stab}</div>
        <div class="wan-speed">${s.speed_mbps ? fmt(s.speed_mbps) : "–"} Mb/s</div>
        <div class="wan-meta">${fmt(s.conns)} connections</div>
        <div class="wan-meta">${protoTags}</div>
      </div>`;
  }).join("");
}

/* ---------- configs ---------- */

async function loadConfigs() {
  const filter = $("#state-filter").value;
  const q = $("#search").value.trim().toLowerCase();
  try {
    const p = await api("/api/viberayd/configs?page=1&per_page=100" + (filter ? "&state=" + encodeURIComponent(filter) : ""));
    let list = p.configs || [];
    if (q) list = list.filter((c) => (c.host || "").toLowerCase().includes(q));
    renderConfigs(list);
  } catch (e) {
    $("#config-list").innerHTML = `<div class="empty-state">configs unavailable: ${esc(e.message)}</div>`;
  }
}

function renderConfigs(list) {
  const el = $("#config-list");
  if (!list.length) {
    el.innerHTML = `<div class="empty-state">No configs match.</div>`;
    return;
  }
  el.innerHTML = list.map((c) => `
    <div class="config-row">
      <span class="host" title="${esc(c.raw || "")}">${esc(c.host || "?")}:${c.port ?? ""}</span>
      <span class="proto muted">${esc(c.protocol || "")}</span>
      <span class="state muted"><span class="pill ${stateClass(c.state)}">${esc(c.state || "")}</span></span>
      <span class="muted">${c.latency_ms ? c.latency_ms + "ms" : "–"}</span>
      <span class="muted">✓${c.success_count ?? 0} ✗${c.fail_count ?? 0}</span>
    </div>`).join("");
}

/* ---------- settings ---------- */

async function loadSettings() {
  try {
    const [schema, values] = await Promise.all([
      api("/api/config/schema"),
      api("/api/config/values"),
    ]);
    state.schema = schema;
    state.values = values;
    renderSettingsForm();
    loadURLs();
  } catch (e) {
    $("#settings-fields").innerHTML = `<div class="empty-state">settings unavailable: ${esc(e.message)}</div>`;
  }
}

/* ---- Subscription URLs (viberayd) ---- */

async function loadURLs() {
  if (state.group !== "viberayd") return;
  try {
    const res = await api("/api/viberayd/urls");
    $("#urls-card").hidden = false;
    $("#urls-textarea").value = (res.urls || []).join("\n");
    clearURLsMsg();
  } catch (e) {
    // viberayd unreachable: keep the card hidden rather than showing an error
    // that would block the settings form.
    $("#urls-card").hidden = true;
  }
}

function clearURLsMsg() {
  const el = $("#urls-msg");
  el.className = "form-msg";
  el.textContent = "";
}

async function applyURLs() {
  const btn = $("#urls-apply");
  btn.disabled = true;
  clearURLsMsg();
  try {
    const lines = $("#urls-textarea").value.split("\n");
    const res = await api("/api/viberayd/urls", {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ urls: lines }),
    });
    $("#urls-textarea").value = (res.urls || []).join("\n");
    $("#urls-msg").className = "form-msg";
    $("#urls-msg").textContent = "URLs applied (" + (res.urls || []).length + ")";
    toast("Subscription URLs updated", "ok");
  } catch (e) {
    $("#urls-msg").className = "form-msg err";
    $("#urls-msg").textContent = "error: " + e.message;
    toast("URLs apply failed: " + e.message, "err");
  } finally {
    btn.disabled = false;
  }
}

function renderSettingsForm() {
  const fields = (state.schema.groups[state.group] || []);
  const values = state.values[state.group] || {};
  $("#settings-fields").innerHTML = `
    <div class="form-grid">
      ${fields.map(fieldHTML(fields, values)).join("")}
    </div>`;
}

function fieldHTML(fields, values) {
  return (f) => {
    const v = values[f.key] ?? f.default ?? "";
    const req = f.required ? '<span class="req">*</span>' : "";
    const desc = f.description ? `<div class="desc">${esc(f.description)}</div>` : "";
    let input;
    if (f.type === "bool") {
      input = `<select id="f-${f.key}" class="input" data-key="${f.key}">
        <option value="true" ${v === "true" ? "selected" : ""}>true</option>
        <option value="false" ${v === "false" || v === "" ? "selected" : ""}>false</option>
      </select>`;
    } else if (f.type === "enum") {
      input = `<select id="f-${f.key}" class="input" data-key="${f.key}">
        ${f.enum.map((e) => `<option value="${e}" ${v === e ? "selected" : ""}>${e}</option>`).join("")}
      </select>`;
    } else {
      const type = f.type === "int" ? "number" : "text";
      input = `<input id="f-${f.key}" class="input" type="${type}" data-key="${f.key}"
        value="${esc(v)}" placeholder="${esc(f.placeholder || "")}"
        ${f.min != null ? `min="${f.min}"` : ""} ${f.max != null ? `max="${f.max}"` : ""}>`;
    }
    return `
      <div class="field" data-group="${f.group}">
        <label for="f-${f.key}">${esc(f.label)} ${req}</label>
        ${desc}
        ${input}
        <div class="err" id="e-${f.key}"></div>
      </div>`;
  };
}

function collectFormValues() {
  const out = {};
  document.querySelectorAll("#settings-fields [data-key]").forEach((el) => {
    out[el.dataset.key] = el.value;
  });
  return out;
}

async function saveSettings(ev) {
  ev.preventDefault();
  const btn = $('button[type="submit"]');
  btn.disabled = true;
  $("#form-msg").className = "form-msg";
  $("#form-msg").textContent = "saving…";
  try {
    const res = await api("/api/config/values", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        group: state.group,
        values: collectFormValues(),
        restart: $("#restart-check").checked,
      }),
    });
    const parts = [`saved ${res.path || ""}`];
    if (res.restarted && res.restarted.length) parts.push("restarted " + res.restarted.join(", "));
    $("#form-msg").className = "form-msg";
    $("#form-msg").textContent = parts.join(" · ");
    toast("Configuration saved" + (res.restarted?.length ? " & restarted" : ""), "ok");
    await loadSettings();
    setTimeout(loadOverview, 1200); // let daemons come back
  } catch (e) {
    $("#form-msg").className = "form-msg err";
    $("#form-msg").textContent = "error: " + e.message;
    toast("save failed: " + e.message, "err");
  } finally {
    btn.disabled = false;
  }
}

/* ---------- boot ---------- */

function init() {
  initTheme();

  $("#refresh").addEventListener("click", () => {
    loadOverview();
    loadConfigs();
  });
  $("#state-filter").addEventListener("change", loadConfigs);
  $("#search").addEventListener("input", debounce(loadConfigs, 250));
  $("#settings-form").addEventListener("submit", saveSettings);
  $("#urls-apply").addEventListener("click", applyURLs);

  document.querySelectorAll(".tab").forEach((tab) => {
    tab.addEventListener("click", () => {
      document.querySelectorAll(".tab").forEach((t) => t.classList.remove("active"));
      tab.classList.add("active");
      state.group = tab.dataset.group;
      renderSettingsForm();
      loadURLs(); // shows the URLs card only on the viberayd tab
    });
  });

  loadOverview();
  loadConfigs();
  loadSettings();

  setInterval(() => {
    loadOverview();
    if (!document.hidden) loadConfigs();
  }, POLL_MS);
}

function debounce(fn, ms) {
  let t;
  return (...args) => { clearTimeout(t); t = setTimeout(() => fn(...args), ms); };
}

document.addEventListener("DOMContentLoaded", init);
