"use strict";
(() => {
  const $ = (id) => document.getElementById(id);
  const state = {
    sessions: [],
    csrf: "",
    page: "overview",
    eventCursor: 0,
    messageCursor: 0,
    refreshing: false,
  };
  let qrURL = "";
  const labels = {
    overview: "Overview",
    sessions: "Sessions",
    events: "Event log",
    history: "Messages",
    settings: "Settings",
  };
  function node(tag, text, className) {
    const el = document.createElement(tag);
    if (text !== undefined) el.textContent = String(text);
    if (className) el.className = className;
    return el;
  }
  function notify(message, error = false) {
    $("notice").textContent = message;
    $("notice").className = error ? "error" : "";
    $("notice").hidden = false;
  }
  function closeQR() {
    if ($("qr-dialog").open) $("qr-dialog").close();
    if (qrURL) URL.revokeObjectURL(qrURL);
    qrURL = "";
    $("qr-image").removeAttribute("src");
  }
  function lock() {
    $("app").hidden = true;
    $("login").hidden = false;
    state.csrf = "";
    state.sessions = [];
    closeQR();
    [
      "session-rows",
      "event-rows",
      "message-rows",
      "health-list",
      "capability-list",
    ].forEach((id) => $(id).replaceChildren());
    $("event-detail").textContent = "Select an event to view its data.";
    $("pair-code").textContent = "";
  }
  async function request(path, method = "GET", body) {
    const headers = {};
    if (body !== undefined) headers["Content-Type"] = "application/json";
    if (method !== "GET" && state.csrf) headers["X-CSRF-Token"] = state.csrf;
    const response = await fetch(path, {
      method,
      headers,
      credentials: "same-origin",
      body: body === undefined ? undefined : JSON.stringify(body),
    });
    const result = await response.json();
    if (!response.ok) {
      if (response.status === 401 && !path.endsWith("/login")) lock();
      throw new Error(result.message || `Request failed (${response.status})`);
    }
    return result;
  }
  async function busy(form, action) {
    const button =
      form.querySelector('button[type="submit"]') ||
      form.querySelector("button");
    if (button) button.disabled = true;
    try {
      await action();
    } catch (error) {
      notify(error.message, true);
    } finally {
      if (button) button.disabled = false;
    }
  }
  function page(name) {
    if (!labels[name]) return;
    Object.keys(labels).forEach((id) => ($(id).hidden = id !== name));
    document.querySelectorAll("[data-page]").forEach((el) => {
      if (el.dataset.page === name) el.setAttribute("aria-current", "page");
      else el.removeAttribute("aria-current");
    });
    state.page = name;
    $("page-title").textContent = labels[name];
    $("page-title").focus();
  }
  function pickerValues() {
    document.querySelectorAll(".session-picker").forEach((select) => {
      const previous = select.value;
      select.replaceChildren();
      const empty = node("option", "Choose a session");
      empty.value = "";
      select.append(empty);
      state.sessions.forEach((item) => {
        const option = node("option", item.name);
        option.value = item.name;
        select.append(option);
      });
      if (state.sessions.some((item) => item.name === previous))
        select.value = previous;
    });
  }
  function statusPill(status) {
    return node(
      "span",
      status,
      "pill " +
        (status === "CONNECTED"
          ? "success"
          : status === "QRCODE" || status === "INITIALIZING"
            ? "warning"
            : "neutral"),
    );
  }
  function actionButton(text, callback, dangerous = false) {
    const button = node("button", text, dangerous ? "quiet danger" : "quiet");
    button.type = "button";
    button.addEventListener("click", async () => {
      button.disabled = true;
      try {
        await callback();
      } catch (error) {
        notify(error.message, true);
      } finally {
        button.disabled = false;
      }
    });
    return button;
  }
  async function sessionAction(name, action) {
    if (
      action === "logout-session" &&
      !window.confirm(
        `Revoke the linked device for ${name}? A new pairing will be required.`,
      )
    )
      return;
    await request(`/api/${encodeURIComponent(name)}/${action}`, "POST", {});
    notify(
      `${name}: ${action === "close-session" ? "closed" : action === "logout-session" ? "logged out" : "start requested"}.`,
    );
    await refresh();
  }
  async function showQR(name) {
    const response = await fetch(
      `/api/${encodeURIComponent(name)}/qrcode-session`,
      { credentials: "same-origin" },
    );
    if (
      !response.ok ||
      !(response.headers.get("Content-Type") || "").includes("image/png")
    )
      throw new Error("No current QR code. Start the session, then try again.");
    closeQR();
    qrURL = URL.createObjectURL(await response.blob());
    $("qr-image").src = qrURL;
    $("qr-dialog").showModal();
  }
  function renderSessions() {
    const filter = $("session-search").value.toLowerCase();
    $("session-rows").replaceChildren();
    $("health-list").replaceChildren();
    const list = state.sessions.filter((item) =>
      item.name.toLowerCase().includes(filter),
    );
    $("session-empty").hidden = list.length > 0;
    list.forEach((item) => {
      const row = node("tr"),
        name = node("td", item.name, "session-name"),
        status = node("td"),
        config = node("td"),
        actions = node("td");
      status.append(statusPill(item.status));
      config.append(
        node(
          "small",
          item.settings.hasProxy ? "Proxy configured" : "Direct connection",
        ),
      );
      if (item.settings.autoRead) config.append(node("small", "Auto-read on"));
      actions.append(
        actionButton("Start", () => sessionAction(item.name, "start-session")),
        actionButton("QR", () => showQR(item.name)),
        actionButton("Close", () => sessionAction(item.name, "close-session")),
        actionButton(
          "Log out",
          () => sessionAction(item.name, "logout-session"),
          true,
        ),
        actionButton("Settings", async () => {
          $("settings-session").value = item.name;
          page("settings");
          await loadSettings();
        }),
      );
      row.append(name, status, config, actions);
      $("session-rows").append(row);
    });
    state.sessions.slice(0, 8).forEach((item) => {
      const row = node("div", undefined, "health-row"),
        text = node("div");
      text.append(
        node("strong", item.name),
        node(
          "small",
          item.settings.hasProxy ? "Proxy connection" : "Direct connection",
        ),
      );
      row.append(text, statusPill(item.status));
      $("health-list").append(row);
    });
    if (!state.sessions.length) {
      const empty = node("div", undefined, "empty");
      empty.append(
        node("h3", "Ready for your first connection."),
        node("p", "Create a session in the Sessions workspace."),
      );
      $("health-list").append(empty);
    }
  }
  async function refresh() {
    if (state.refreshing || $("app").hidden) return;
    state.refreshing = true;
    $("refresh").disabled = true;
    try {
      const [sessions, capabilities, metrics] = await Promise.all([
        request("/api/admin/sessions"),
        request("/api/admin/capabilities"),
        fetch("/metrics", { credentials: "same-origin" }).then(
          async (response) => {
            if (!response.ok) throw new Error("Metrics are unavailable.");
            return response.text();
          },
        ),
      ]);
      state.sessions = sessions.response;
      pickerValues();
      renderSessions();
      $("metric-total").textContent = String(state.sessions.length);
      $("metric-connected").textContent = String(
        state.sessions.filter((item) => item.status === "CONNECTED").length,
      );
      ["pending", "failed"].forEach((key) => {
        const match = metrics.match(
          new RegExp(`wppconnect_event_deliveries\\{state="${key}"\\} (\\d+)`),
        );
        $("metric-" + key).textContent = match ? match[1] : "-";
      });
      $("updated").textContent = "Updated " + new Date().toLocaleTimeString();
      $("capability-list").replaceChildren();
      const names = {
        nativeMessaging: "Native messages",
        messageActions: "Message actions",
        history: "Observed history",
        accountPrivacy: "Account and privacy",
        labels: "Labels",
        phonePairing: "Phone pairing",
        durableEvents: "Durable event log",
        s3Media: "S3 media archive",
        websocket: "WebSocket events",
        manager: "Management console",
        universalInteractiveDelivery: "Universal interactive delivery",
      };
      Object.entries(capabilities.response).forEach(([key, enabled]) => {
        const row = node("div");
        row.append(
          node("span", names[key] || key),
          node(
            "span",
            enabled ? "Available" : "Unavailable",
            "pill " + (enabled ? "success" : "neutral"),
          ),
        );
        $("capability-list").append(row);
      });
    } finally {
      state.refreshing = false;
      $("refresh").disabled = false;
    }
  }
  async function openConsole(csrf) {
    state.csrf = csrf;
    $("login").hidden = true;
    $("app").hidden = false;
    $("login-error").textContent = "";
    await refresh();
    page("overview");
  }
  async function loadSettings() {
    const name = $("settings-session").value;
    if (!name) return;
    const result = await request(
        `/api/admin/session-settings/${encodeURIComponent(name)}`,
      ),
      value = result.response;
    $("auto-read").checked = value.autoRead;
    $("always-online").checked = value.alwaysOnline;
    $("proxy-url").value = "";
    $("clear-proxy").checked = false;
    $("proxy-state").textContent = value.hasProxy
      ? "A proxy is configured. Its credentials are hidden."
      : "This session uses a direct connection.";
  }
  function boundedRows(id) {
    while ($(id).children.length > 2000) $(id).firstElementChild.remove();
  }
  async function loadEvents(reset) {
    const name = $("event-session").value;
    if (!name) return;
    if (reset) {
      state.eventCursor = 0;
      $("event-rows").replaceChildren();
    }
    const result = await request(
      `/api/${encodeURIComponent(name)}/events?cursor=${state.eventCursor}&limit=50`,
    );
    result.response.forEach((event) => {
      const row = node("tr"),
        actions = node("td");
      actions.append(
        actionButton("Inspect", async () => {
          $("event-detail").textContent = JSON.stringify(event.data, null, 2);
          $("event-detail").parentElement.open = true;
        }),
        actionButton("Replay", async () => {
          await request(
            `/api/${encodeURIComponent(name)}/events/replay`,
            "POST",
            { eventId: event.id },
          );
          notify(
            "Event queued for configured delivery targets with its original ID.",
          );
        }),
      );
      row.append(
        node("td", new Date(event.createdAt).toLocaleString()),
        node("td", event.event),
        node("td", event.id),
        actions,
      );
      $("event-rows").append(row);
    });
    state.eventCursor = result.nextCursor;
    $("event-empty").hidden = $("event-rows").children.length > 0;
    $("more-events").hidden = result.response.length === 0;
    boundedRows("event-rows");
  }
  async function loadMessages(reset) {
    const name = $("history-session").value;
    if (!name) return;
    if (reset) {
      state.messageCursor = 0;
      $("message-rows").replaceChildren();
    }
    const query = new URLSearchParams({ limit: "50" });
    if (state.messageCursor) query.set("cursor", String(state.messageCursor));
    if ($("history-chat").value) query.set("chatId", $("history-chat").value);
    const result = await request(
      `/api/${encodeURIComponent(name)}/messages?${query}`,
    );
    result.response.forEach((message) => {
      const row = node("tr"),
        identity = node("td");
      identity.append(node("span", message.chatId), node("small", message.id));
      const content = message.content || {};
      row.append(
        node("td", new Date(message.t * 1000).toLocaleString()),
        identity,
        node("td", message.deleted ? "deleted" : content.type || "chat"),
        node(
          "td",
          String(content.body || content.name || content.title || "").slice(
            0,
            500,
          ),
          "message-cell",
        ),
      );
      $("message-rows").append(row);
    });
    state.messageCursor = result.nextCursor;
    $("history-empty").hidden = $("message-rows").children.length > 0;
    $("older-messages").hidden = !result.nextCursor;
    boundedRows("message-rows");
  }
  document
    .querySelectorAll("[data-page]")
    .forEach((button) =>
      button.addEventListener("click", () => page(button.dataset.page)),
    );
  document
    .querySelectorAll("[data-jump]")
    .forEach((button) =>
      button.addEventListener("click", () => page(button.dataset.jump)),
    );
  $("login-form").addEventListener("submit", async (event) => {
    event.preventDefault();
    const button = event.currentTarget.querySelector("button");
    button.disabled = true;
    try {
      const result = await request("/api/admin/login", "POST", {
        token: $("admin-token").value,
      });
      $("admin-token").value = "";
      await openConsole(result.csrfToken);
    } catch (error) {
      $("login-error").textContent = error.message;
      $("admin-token").focus();
    } finally {
      button.disabled = false;
    }
  });
  $("logout").addEventListener("click", async () => {
    try {
      await request("/api/admin/logout", "POST", {});
      lock();
    } catch (error) {
      notify(error.message, true);
    }
  });
  $("refresh").addEventListener("click", () =>
    refresh().catch((error) => notify(error.message, true)),
  );
  $("session-search").addEventListener("input", renderSessions);
  $("create-form").addEventListener("submit", (event) => {
    event.preventDefault();
    busy(event.currentTarget, async () => {
      const name = $("new-name").value;
      await request("/api/admin/sessions", "POST", { name });
      $("new-name").value = "";
      notify("Session started. Request a QR code or pair by phone.");
      await refresh();
    });
  });
  $("pair-form").addEventListener("submit", (event) => {
    event.preventDefault();
    busy(event.currentTarget, async () => {
      $("pair-code").textContent = "";
      const result = await request(
        `/api/${encodeURIComponent($("pair-session").value)}/pair-phone`,
        "POST",
        { phone: $("pair-phone").value },
      );
      $("pair-code").textContent = result.response.code;
    });
  });
  $("events-form").addEventListener("submit", (event) => {
    event.preventDefault();
    busy(event.currentTarget, () => loadEvents(true));
  });
  $("more-events").addEventListener("click", () =>
    loadEvents(false).catch((error) => notify(error.message, true)),
  );
  $("history-form").addEventListener("submit", (event) => {
    event.preventDefault();
    busy(event.currentTarget, () => loadMessages(true));
  });
  $("older-messages").addEventListener("click", () =>
    loadMessages(false).catch((error) => notify(error.message, true)),
  );
  $("send-form").addEventListener("submit", (event) => {
    event.preventDefault();
    busy(event.currentTarget, async () => {
      const payload = {
        phone: $("send-phone").value,
        message: $("send-message").value,
        isGroup: $("send-group").checked,
      };
      if ($("send-quote").value)
        payload.quotedMessageId = $("send-quote").value;
      const result = await request(
        `/api/${encodeURIComponent($("send-session").value)}/send-message`,
        "POST",
        payload,
      );
      notify(
        "Message accepted: " +
          result.response.map((item) => item.id).join(", "),
      );
      $("send-message").value = "";
    });
  });
  $("settings-session").addEventListener("change", () =>
    loadSettings().catch((error) => notify(error.message, true)),
  );
  $("settings-form").addEventListener("submit", (event) => {
    event.preventDefault();
    busy(event.currentTarget, async () => {
      const payload = {
        autoRead: $("auto-read").checked,
        alwaysOnline: $("always-online").checked,
      };
      if ($("clear-proxy").checked) payload.proxyUrl = "";
      else if ($("proxy-url").value) payload.proxyUrl = $("proxy-url").value;
      await request(
        `/api/admin/session-settings/${encodeURIComponent($("settings-session").value)}`,
        "PUT",
        payload,
      );
      notify(
        "Settings saved. Proxy changes apply after closing and restarting the session.",
      );
      await loadSettings();
      await refresh();
    });
  });
  $("close-qr").addEventListener("click", closeQR);
  $("qr-dialog").addEventListener("close", () => {
    if (qrURL) URL.revokeObjectURL(qrURL);
    qrURL = "";
  });
  request("/api/admin/auth")
    .then((result) => openConsole(result.csrfToken))
    .catch((error) => {
      if (!error.message.includes("authentication required"))
        $("login-error").textContent = error.message;
    });
  setInterval(() => {
    if (!$("app").hidden && !document.hidden)
      refresh().catch((error) => notify(error.message, true));
  }, 15000);
})();
