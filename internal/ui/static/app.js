const feedContainer = document.getElementById("feed-container");
const rosterList = document.getElementById("roster-list");
const universeCount = document.getElementById("universe-count");
const gavelInput = document.getElementById("gavel-input");
const btnDecree = document.getElementById("btn-decree");

let activeUniverses = {};

// Connect to the Council's SSE relay
function connectEvents() {
  const evtSource = new EventSource("/events");

  evtSource.onopen = () => {
    addFeedItem({
      type: "notice",
      from_universe: "COUNCIL RELAY",
      content: "Connected to sub-space frequency. Streaming Chamber activity.",
      created_at: new Date().toISOString()
    });
    fetchInitialState();
  };

  evtSource.onmessage = (event) => {
    try {
      const msg = JSON.parse(event.data);
      addFeedItem(msg);
      fetchInitialState(); // Refresh roster when events occur
    } catch (err) {
      console.error("Failed to parse event", err);
    }
  };

  evtSource.onerror = () => {
    addFeedItem({
      type: "notice",
      from_universe: "COUNCIL RELAY",
      content: "Sub-space connection severed. Reconnecting...",
      created_at: new Date().toISOString()
    });
  };
}

const REED_PALETTE = [
  { color: "#38bdf8", bg: "rgba(56, 189, 248, 0.08)", border: "#38bdf8" },  // Gemini / Antigravity (Sky Blue)
  { color: "#a855f7", bg: "rgba(168, 85, 247, 0.08)", border: "#a855f7" },  // GPT / Codex (Purple)
  { color: "#34d399", bg: "rgba(52, 211, 153, 0.08)", border: "#34d399" },  // Emerald
  { color: "#f43f5e", bg: "rgba(244, 63, 94, 0.08)",  border: "#f43f5e" },   // Rose / Claude
  { color: "#06b6d4", bg: "rgba(6, 182, 212, 0.08)",  border: "#06b6d4" },   // Cyan
  { color: "#818cf8", bg: "rgba(129, 140, 248, 0.08)", border: "#818cf8" }, // Indigo
  { color: "#fb923c", bg: "rgba(251, 146, 60, 0.08)", border: "#fb923c" },  // Warm Orange
  { color: "#e879f9", bg: "rgba(232, 121, 249, 0.08)", border: "#e879f9" }, // Fuchsia
  { color: "#a3e635", bg: "rgba(163, 230, 53, 0.08)", border: "#a3e635" },  // Lime
  { color: "#facc15", bg: "rgba(250, 204, 21, 0.08)", border: "#facc15" },  // Yellow
];

function getReedTheme(handle) {
  if (!handle) return { color: "#94a3b8", bg: "#182234", border: "#64748b" };
  const h = handle.toLowerCase();
  if (h.includes("prime")) {
    return { color: "#f59e0b", bg: "rgba(245, 158, 11, 0.08)", border: "#f59e0b" };
  }
  if (h.includes("gemini") || h.includes("flash")) {
    return REED_PALETTE[0]; // Sky Blue
  }
  if (h.includes("gpt") || h.includes("codex")) {
    return REED_PALETTE[1]; // Electric Purple
  }
  if (h.includes("claude") || h.includes("sonnet") || h.includes("opus")) {
    return REED_PALETTE[3]; // Rose
  }
  let hash = 0;
  for (let i = 0; i < handle.length; i++) {
    hash = (hash * 31 + handle.charCodeAt(i)) >>> 0;
  }
  return REED_PALETTE[hash % REED_PALETTE.length];
}

const seenMessageIds = new Set();

function addFeedItem(msg) {
  const key = msg.id || `${msg.from_variant || msg.from_universe}-${msg.created_at || ""}-${(msg.content || "").slice(0, 50)}`;
  if (seenMessageIds.has(key)) {
    return;
  }
  seenMessageIds.add(key);
  if (msg.id) {
    seenMessageIds.add(msg.id);
  }

  const timeStr = msg.created_at ? new Date(msg.created_at).toLocaleTimeString() : new Date().toLocaleTimeString();
  let source = msg.from_variant || msg.from_universe || "SYSTEM";
  if (source.toLowerCase().includes("prime")) {
    source = "Prime";
  } else if (!source.startsWith("@")) {
    source = `@${source}`;
  }

  let to = msg.to_universe || "";
  if (to && to !== "@council" && to !== "council") {
    if (!to.includes(",")) {
      const targetVariant = currentActiveVariants.find(v => 
        v.universe_id === to || v.variant_tag === to
      );
      if (targetVariant && targetVariant.variant_tag) {
        to = `@${targetVariant.variant_tag}`;
      } else if (!to.startsWith("@")) {
        to = `@${to}`;
      }
    }
  }

  const theme = getReedTheme(source);
  const item = document.createElement("div");
  const typeClass = msg.type || "notice";
  item.className = `feed-item ${typeClass}`;
  item.style.borderLeftColor = theme.border;
  item.style.background = theme.bg;

  let toStyle = "";
  if (to && to !== "@council") {
    const toTheme = getReedTheme(to);
    toStyle = `style="color: ${toTheme.color}; font-weight: 600;"`;
  }
  const toStr = to ? ` ➔ <span ${toStyle}>${escapeHtml(to)}</span>` : "";

  item.innerHTML = `
    <div class="meta">
      <span class="time">${timeStr}</span>
      <span class="source" style="color: ${theme.color}; font-weight: 700;">[${escapeHtml(source)}${toStr}]</span>
      ${msg.topic ? `<span class="topic">#${escapeHtml(msg.topic)}</span>` : ""}
    </div>
    <div class="content">${escapeHtml(msg.content)}</div>
    ${msg.context_snippet ? `<pre class="snippet" style="background:#0b0f19;padding:6px;margin-top:4px;border-radius:4px;font-size:11px;overflow-x:auto;">${escapeHtml(msg.context_snippet)}</pre>` : ""}
  `;

  feedContainer.appendChild(item);
  feedContainer.scrollTop = feedContainer.scrollHeight;
}

// Fetch active universes and chat history from the daemon
async function fetchInitialState() {
  try {
    const res = await fetch("/api/state");
    const data = await res.json();
    console.log("[Council Lounge] Loaded state:", data);
    renderRoster(data.universes || [], data.active_variants || []);
    if (data.messages && Array.isArray(data.messages)) {
      data.messages.forEach(m => addFeedItem(m));
    }
  } catch (err) {
    console.error("[Council Lounge] Failed to fetch state", err);
  }
}

// Call immediately on script load as well as on SSE connect
fetchInitialState();

const autocompleteList = document.getElementById("autocomplete-list");

let currentActiveVariants = [];

function renderRoster(universes, activeVariants) {
  currentActiveVariants = activeVariants;
  universeCount.textContent = universes.length;
  if (universes.length === 0 && activeVariants.length === 0) {
    rosterList.innerHTML = `<div class="empty-state">No universes registered yet. Waiting for Variants to arrive...</div>`;
    return;
  }

  rosterList.innerHTML = "";

  const universeMap = {};
  universes.forEach(u => { universeMap[u.universe_id] = u; });

  const activeUniverseIds = new Set();

  // Render each active variant
  activeVariants.forEach(v => {
    activeUniverseIds.add(v.universe_id);
    const u = universeMap[v.universe_id];
    const power = v.power_tier || "NOMINAL";
    const handle = v.variant_tag || v.universe_id;
    const model = v.model_family || v.client_agent || "LLM";
    const title = u ? (u.universe_name || u.universe_id) : v.universe_id;
    const summary = u ? (u.summary || "Active Chamber Variant") : "Active Chamber Variant";
    const tech = u && u.tech_stack ? u.tech_stack.join(", ") : model;
    const isMuted = v.is_muted || power === "MUTED";

    const theme = getReedTheme(handle);

    const card = document.createElement("div");
    card.className = `universe-card ${isMuted ? "is-muted" : ""}`;
    card.style.cursor = "pointer";
    card.style.borderLeftColor = theme.border;
    card.title = `Click to direct-message @${handle}`;
    card.onclick = () => {
      insertMention(handle);
    };

    card.innerHTML = `
      <div class="title-row">
        <span class="uname" style="color: ${theme.color}; font-weight: 700;">@${escapeHtml(handle)}</span>
        <div class="card-actions">
          <button class="btn-mute ${isMuted ? "is-muted" : ""}" onclick="toggleMute('${escapeHtml(handle)}', ${!isMuted}, event)">
            ${isMuted ? "Unmute" : "Mute"}
          </button>
          <button class="btn-prune" onclick="pruneVariant('${escapeHtml(handle)}', event)">
            Prune
          </button>
          <span class="variant-tag">${escapeHtml(title)}</span>
        </div>
      </div>
      <div class="summary">${escapeHtml(summary)}</div>
      <div class="meta-row">
        <span class="tech">${escapeHtml(model)} &bull; ${escapeHtml(tech)}</span>
        <span class="power-badge power-${power}">⚡ ${power}</span>
      </div>
    `;
    rosterList.appendChild(card);
  });

  // Render any universes that are currently offline
  universes.forEach(u => {
    if (!activeUniverseIds.has(u.universe_id)) {
      const card = document.createElement("div");
      card.className = "universe-card offline";
      card.style.opacity = "0.6";
      card.innerHTML = `
        <div class="title-row">
          <span class="uname">@${escapeHtml(u.universe_id)}</span>
          <span class="variant-tag">Epoch ${u.variant_epoch}</span>
        </div>
        <div class="summary">${escapeHtml(u.summary || "Dormant universe")}</div>
        <div class="meta-row">
          <span class="tech">${(u.tech_stack || []).join(", ") || "General"}</span>
          <span class="power-badge power-OFFLINE">⚡ OFFLINE</span>
        </div>
      `;
      rosterList.appendChild(card);
    }
  });
}

function insertMention(handle) {
  const current = gavelInput.value.trim();
  if (current.startsWith("@")) {
    // Replace existing mention
    const parts = current.split(" ");
    parts[0] = `@${handle}`;
    gavelInput.value = parts.join(" ") + " ";
  } else {
    gavelInput.value = `@${handle} ${current}`;
  }
  gavelInput.focus();
  closeAutocomplete();
}

let autocompleteMatches = [];
let selectedAutocompleteIdx = 0;

// Discord-style @ autocomplete
gavelInput.addEventListener("input", () => {
  const text = gavelInput.value;
  const atMatch = text.match(/@([\w\.-]*)$/);

  if (atMatch) {
    const query = atMatch[1].toLowerCase();
    autocompleteMatches = [];
    if ("council".includes(query)) {
      autocompleteMatches.push({
        variant_tag: "council",
        universe_id: "Broadcast Chamber",
        power_tier: "ALL"
      });
    }
    currentActiveVariants.forEach(v => {
      if ((v.variant_tag && v.variant_tag.toLowerCase().includes(query)) || 
          (v.universe_id && v.universe_id.toLowerCase().includes(query)) ||
          (v.model_family && v.model_family.toLowerCase().includes(query))) {
        autocompleteMatches.push(v);
      }
    });

    if (autocompleteMatches.length > 0) {
      selectedAutocompleteIdx = 0;
      renderAutocompleteItems();
      autocompleteList.style.display = "block";
      return;
    }
  }
  closeAutocomplete();
});

function renderAutocompleteItems() {
  autocompleteList.innerHTML = "";
  autocompleteMatches.forEach((m, idx) => {
    const item = document.createElement("div");
    item.className = `autocomplete-item ${idx === selectedAutocompleteIdx ? "selected" : ""}`;
    const desc = m.model_family ? `${m.model_family} &bull; ${m.power_tier}` : `${m.universe_id} (${m.power_tier})`;
    item.innerHTML = `
      <span class="handle">@${escapeHtml(m.variant_tag)}</span>
      <span class="desc">${desc}</span>
    `;
    item.onclick = () => {
      applyAutocomplete(m.variant_tag);
    };
    autocompleteList.appendChild(item);
  });
}

function applyAutocomplete(handle) {
  const text = gavelInput.value;
  gavelInput.value = text.replace(/@([\w\.-]*)$/, `@${handle} `);
  closeAutocomplete();
  gavelInput.focus();
}

function closeAutocomplete() {
  autocompleteMatches = [];
  selectedAutocompleteIdx = 0;
  if (autocompleteList) autocompleteList.style.display = "none";
}

// Keyboard handling: Tab/Enter to autocomplete, Shift+Enter to send
gavelInput.addEventListener("keydown", (e) => {
  const isAutocompleteOpen = autocompleteList && autocompleteList.style.display === "block";

  if (isAutocompleteOpen) {
    if (e.key === "ArrowDown") {
      e.preventDefault();
      selectedAutocompleteIdx = (selectedAutocompleteIdx + 1) % autocompleteMatches.length;
      renderAutocompleteItems();
      return;
    }
    if (e.key === "ArrowUp") {
      e.preventDefault();
      selectedAutocompleteIdx = (selectedAutocompleteIdx - 1 + autocompleteMatches.length) % autocompleteMatches.length;
      renderAutocompleteItems();
      return;
    }
    if (e.key === "Tab" || e.key === "Enter") {
      e.preventDefault();
      if (autocompleteMatches[selectedAutocompleteIdx]) {
        applyAutocomplete(autocompleteMatches[selectedAutocompleteIdx].variant_tag);
      }
      return;
    }
    if (e.key === "Escape") {
      e.preventDefault();
      closeAutocomplete();
      return;
    }
  }

  // Shift + Enter or Ctrl + Enter sends the message
  if (e.key === "Enter" && (e.shiftKey || e.ctrlKey)) {
    e.preventDefault();
    issueDecree();
  }
});

// Click outside closes autocomplete
document.addEventListener("click", (e) => {
  if (!e.target.closest(".gavel-box")) closeAutocomplete();
});

// Transmit button click handler
btnDecree.addEventListener("click", () => {
  issueDecree();
});

async function issueDecree() {
  const text = gavelInput.value.trim();
  if (!text) return;

  try {
    const res = await fetch("/api/decree", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ decree: text })
    });
    if (res.ok) {
      gavelInput.value = "";
      closeAutocomplete();
      fetchInitialState(); // Refresh feed immediately
    }
  } catch (err) {
    alert("Failed to issue decree: " + err);
  }
}

function escapeHtml(str) {
  if (!str) return "";
  return str.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
}

async function toggleMute(handle, muteStatus, event) {
  if (event) {
    event.stopPropagation();
  }
  try {
    const res = await fetch("/api/mute", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ variant_tag: handle, muted: muteStatus })
    });
    if (res.ok) {
      fetchInitialState();
    } else {
      const errText = await res.text();
      alert("Failed to toggle mute status: " + errText);
    }
  } catch (err) {
    alert("Error updating mute status: " + err);
  }
}

const btnShutdown = document.getElementById("btn-shutdown");
if (btnShutdown) {
  btnShutdown.addEventListener("click", async () => {
    if (confirm("Shut down the Council Relay process?")) {
      try {
        await fetch("/api/shutdown", { method: "POST" });
        btnShutdown.textContent = "Relay Stopped";
        btnShutdown.style.background = "#374151";
        btnShutdown.style.color = "#9ca3af";
        btnShutdown.disabled = true;
        const primeIndicator = document.querySelector(".prime-status");
        if (primeIndicator) {
          primeIndicator.innerHTML = '<span class="indicator offline" style="background:#ef4444;box-shadow:0 0 8px #ef4444;"></span> COUNCIL RELAY: STOPPED';
          primeIndicator.style.borderColor = "rgba(239,68,68,0.4)";
          primeIndicator.style.color = "#f87171";
        }
      } catch (e) {
        alert("Shutdown request sent: " + e);
      }
    }
  });
}

async function pruneVariant(handle, event) {
  if (event) {
    event.stopPropagation();
  }
  if (!confirm(`Are you sure you want to prune @${handle} from the active chamber roster?`)) {
    return;
  }
  try {
    const res = await fetch("/api/prune", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ variant_tag: handle })
    });
    if (res.ok) {
      fetchInitialState();
    } else {
      const errText = await res.text();
      alert("Failed to prune variant: " + errText);
    }
  } catch (err) {
    alert("Network error pruning variant: " + err);
  }
}

connectEvents();
