// Shared sample data and renderers for the asanamate mocks.
if (location.hash === "#light") document.body.classList.add("light");

const ICON = {
  robot: "\u{F06A9}", check: "", box: "", star: "", folder: "",
  list: "", filter: "", branch: "", clip: "", clock: "",
  user: "", cal: "", warn: "", dot: "", ring: "", inbox: "",
};

const tickets = [
  { g: "Overdue", t: "Fix login redirect loop", s: "In Progress", d: "2d ago", dc: "err", a: "AF", ag: "working" },
  { g: "Overdue", t: "Billing webhook retries", s: "Todo", d: "1d ago", dc: "err", a: "AF" },
  { g: "Today", t: "Update README badges", s: "Review", d: "today", dc: "warn", a: "JD", ag: "waiting" },
  { g: "Today", t: "Rotate staging keys", s: "Todo", d: "today", dc: "warn", a: "AF" },
  { g: "Today", t: "Triage Sentry error spikes", s: "Todo", d: "today", dc: "warn", a: "" },
  { g: "Next 7 days", t: "Dark mode design tokens", s: "Todo", d: "Thu", a: "JD", ag: "done" },
  { g: "Next 7 days", t: "Remove legacy SSO provider", s: "Blocked", d: "Fri", a: "AF" },
  { g: "Next 7 days", t: "Audit npm dependencies", s: "Todo", d: "Sat", a: "MK" },
  { g: "Next 7 days", t: "Write onboarding guide", s: "Review", d: "Mon", a: "JD" },
  { g: "Later", t: "Migrate cron jobs to queue", s: "Backlog", d: "Oct 14", dc: "dim", a: "AF" },
  { g: "Later", t: "Design system audit", s: "Backlog", d: "Oct 20", dc: "dim", a: "MK" },
  { g: "Later", t: "Kill legacy /v1 API", s: "Backlog", d: "Nov 1", dc: "dim", a: "AF" },
];

const sectionClass = { "In Progress": "cyan", Review: "mag", Blocked: "err", Backlog: "dim", Todo: "" };
const agentBadge = {
  working: `<span class="dim">${ICON.robot}</span> <span class="ok">⠹</span>`,
  waiting: `<span class="dim">${ICON.robot}</span> <span class="warn">${ICON.warn}</span>`,
  done: `<span class="dim">${ICON.robot}</span> <span class="ok">${ICON.dot}</span>`,
};

// ticketRows renders the grouped, column-aligned list; sel is the selected index.
function ticketRows(sel = 0, limit = tickets.length) {
  const counts = {};
  tickets.forEach((t) => (counts[t.g] = (counts[t.g] || 0) + 1));
  let html = "", last = "";
  tickets.slice(0, limit).forEach((t, i) => {
    if (t.g !== last) html += `<div class="rule">${t.g} <span class="n">${counts[t.g]}</span></div>`;
    last = t.g;
    const title = t.ag === "waiting" ? `<span class="warn">${t.t}</span>` : t.t;
    html += `<div class="row${i === sel ? " sel" : ""}">
      <span class="mk">${i === sel ? "▌" : ""}</span><span class="dim">${ICON.box}</span>
      <span class="t">${title}</span><span class="${sectionClass[t.s]}">${t.s}</span>
      <span class="${t.dc || ""}">${t.d}</span>
      <span>${t.a ? `<span class="av ${t.a}">${t.a}</span>` : '<span class="dim"> —</span>'}</span>
      <span>${agentBadge[t.ag] || ""}</span></div>`;
  });
  return html;
}

// statusline renders the bottom bar with a mode pill.
function statusline(mode = "NORMAL", color = "var(--accent)", hints = [["enter", "act"], ["e", "edit"], ["/", "filter"], ["p", "proj"], ["?", "keys"]]) {
  return `<div class="status">
    <span class="pill" style="background:${color}">${mode}</span>
    <span class="seg">${ICON.inbox} My Tasks</span><span class="seg">${ICON.filter} is:open</span>
    <span class="seg">${ICON.list} due</span><span class="seg">12/40</span>
    <span class="seg"><span class="ok">⠹2</span> <span class="warn">${ICON.warn} 1</span> <span class="ok">${ICON.dot} 4</span></span>
    <span class="grow"></span>
    ${hints.map(([k, d]) => `<span class="hint"><span class="key">${k}</span> <span class="d">${d}</span></span>`).join("")}
    <span class="seg">${ICON.clock} 14:02</span></div>`;
}

function fill(id, html) { document.getElementById(id).innerHTML = html; }

// expandIcons swaps ${ICON.name} placeholders written in static HTML for glyphs.
function expandIcons() {
  document.body.innerHTML = document.body.innerHTML.replace(/\$\{ICON\.(\w+)\}/g, (_, k) => ICON[k]);
}
