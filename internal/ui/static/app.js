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

// Fetch active universes from the daemon
async function fetchInitialState() {
  try {
    const res = await fetch("/api/state");
    const data = await res.json();
    renderRoster(data.universes || [], data.active_variants || []);
  } catch (err) {
    console.error("Failed to fetch state", err);
  }
}

const targetSelect = document.getElementById("target-select");

function renderRoster(universes, activeVariants) {
  const activeMap = {};
  activeVariants.forEach(v => { activeMap[v.universe_id] = v; });

  // Update target dropdown
  const currentVal = targetSelect ? targetSelect.value : "@council";
  if (targetSelect) {
    targetSelect.innerHTML = `<option value="@council">Broadcast: @council</option>`;
    universes.forEach(u => {
      const opt = document.createElement("option");
      opt.value = u.universe_id;
      opt.textContent = `@${u.universe_id}`;
      targetSelect.appendChild(opt);
    });
    targetSelect.value = currentVal;
  }

  universeCount.textContent = universes.length;
  if (universes.length === 0) {
    rosterList.innerHTML = `<div class="empty-state">No universes registered yet. Waiting for Variants to arrive...</div>`;
    return;
  }

  rosterList.innerHTML = "";
  universes.forEach(u => {
    const active = activeMap[u.universe_id];
    const power = active ? active.power_tier : "OFFLINE";
    const variantTag = active ? active.variant_tag : `Epoch ${u.variant_epoch} (Offline)`;

    const card = document.createElement("div");
    card.className = "universe-card";
    card.style.cursor = "pointer";
    card.title = "Click to target this universe in Gavel";
    card.onclick = () => {
      if (targetSelect) targetSelect.value = u.universe_id;
      gavelInput.focus();
    };

    card.innerHTML = `
      <div class="title-row">
        <span class="uname">${escapeHtml(u.universe_name || u.universe_id)}</span>
        <span class="variant-tag">${escapeHtml(variantTag)}</span>
      </div>
      <div class="summary">${escapeHtml(u.summary || "No summary provided")}</div>
      <div class="meta-row">
        <span class="tech">${(u.tech_stack || []).join(", ") || "General"}</span>
        <span class="power-badge power-${power}">⚡ ${power}</span>
      </div>
    `;
    rosterList.appendChild(card);
  });
}

function addFeedItem(msg) {
  const item = document.createElement("div");
  item.className = `feed-item ${msg.type || "notice"}`;
  
  const timeStr = msg.created_at ? new Date(msg.created_at).toLocaleTimeString() : new Date().toLocaleTimeString();
  const source = msg.from_variant || msg.from_universe || "SYSTEM";
  const to = msg.to_universe ? ` ➔ ${msg.to_universe}` : "";

  item.innerHTML = `
    <div class="meta">
      <span class="time">${timeStr}</span>
      <span class="source">[${escapeHtml(source)}${escapeHtml(to)}]</span>
      ${msg.topic ? `<span class="topic">#${escapeHtml(msg.topic)}</span>` : ""}
    </div>
    <div class="content">${escapeHtml(msg.content)}</div>
    ${msg.context_snippet ? `<pre class="snippet" style="background:#0b0f19;padding:6px;margin-top:4px;border-radius:4px;font-size:11px;overflow-x:auto;">${escapeHtml(msg.context_snippet)}</pre>` : ""}
  `;

  feedContainer.appendChild(item);
  feedContainer.scrollTop = feedContainer.scrollHeight;
}

// Reed Prime Gavel / Decree
btnDecree.addEventListener("click", issueDecree);
gavelInput.addEventListener("keydown", (e) => {
  if (e.key === "Enter") issueDecree();
});

async function issueDecree() {
  const text = gavelInput.value.trim();
  if (!text) return;
  const target = targetSelect ? targetSelect.value : "@council";

  try {
    const res = await fetch("/api/decree", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ decree: text, to_universe: target })
    });
    if (res.ok) {
      gavelInput.value = "";
    }
  } catch (err) {
    alert("Failed to issue decree: " + err);
  }
}

function escapeHtml(str) {
  if (!str) return "";
  return str.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
}

connectEvents();
