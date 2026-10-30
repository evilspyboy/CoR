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
    console.log("[Council Lounge] Loaded state:", data);
    renderRoster(data.universes || [], data.active_variants || []);
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
  const activeMap = {};
  activeVariants.forEach(v => { activeMap[v.universe_id] = v; });

  universeCount.textContent = universes.length;
  if (universes.length === 0) {
    rosterList.innerHTML = `<div class="empty-state">No universes registered yet. Waiting for Variants to arrive...</div>`;
    return;
  }

  rosterList.innerHTML = "";
  universes.forEach(u => {
    const active = activeMap[u.universe_id];
    const power = active ? active.power_tier : "OFFLINE";
    const handle = active && active.variant_tag ? active.variant_tag : `Epoch ${u.variant_epoch}`;

    const card = document.createElement("div");
    card.className = "universe-card";
    card.style.cursor = "pointer";
    card.title = `Click to direct-message @${handle}`;
    card.onclick = () => {
      insertMention(handle);
    };

    card.innerHTML = `
      <div class="title-row">
        <span class="uname">@${escapeHtml(handle)}</span>
        <span class="variant-tag">${escapeHtml(u.universe_name || u.universe_id)}</span>
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
  const atMatch = text.match(/@([\w-]*)$/);

  if (atMatch) {
    const query = atMatch[1].toLowerCase();
    autocompleteMatches = currentActiveVariants.filter(v => 
      (v.variant_tag && v.variant_tag.toLowerCase().includes(query)) || 
      (v.universe_id && v.universe_id.toLowerCase().includes(query))
    );

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
    item.innerHTML = `
      <span class="handle">@${escapeHtml(m.variant_tag)}</span>
      <span class="desc">${escapeHtml(m.universe_id)} (${m.power_tier})</span>
    `;
    item.onclick = () => {
      applyAutocomplete(m.variant_tag);
    };
    autocompleteList.appendChild(item);
  });
}

function applyAutocomplete(handle) {
  const text = gavelInput.value;
  gavelInput.value = text.replace(/@([\w-]*)$/, `@${handle} `);
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

connectEvents();
