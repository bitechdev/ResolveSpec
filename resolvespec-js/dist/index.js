import { v4 as l } from "uuid";
const d = /* @__PURE__ */ new Map();
function E(n) {
  const e = n.baseUrl;
  let t = d.get(e);
  return t || (t = new g(n), d.set(e, t)), t;
}
class g {
  constructor(e) {
    this.config = e;
  }
  buildUrl(e, t, s) {
    let r = `${this.config.baseUrl}/${e}/${t}`;
    return s && (r += `/${s}`), r;
  }
  baseHeaders() {
    const e = {
      "Content-Type": "application/json"
    };
    return this.config.token && (e.Authorization = `Bearer ${this.config.token}`), e;
  }
  async fetchWithError(e, t) {
    const s = await fetch(e, t), r = await s.json();
    if (!s.ok)
      throw new Error(r.error?.message || "An error occurred");
    return r;
  }
  async getMetadata(e, t) {
    const s = this.buildUrl(e, t);
    return this.fetchWithError(s, {
      method: "GET",
      headers: this.baseHeaders()
    });
  }
  async read(e, t, s, r) {
    const i = typeof s == "number" || typeof s == "string" ? String(s) : void 0, a = this.buildUrl(e, t, i), c = {
      operation: "read",
      id: Array.isArray(s) ? s : void 0,
      options: r
    };
    return this.fetchWithError(a, {
      method: "POST",
      headers: this.baseHeaders(),
      body: JSON.stringify(c)
    });
  }
  async create(e, t, s, r) {
    const i = this.buildUrl(e, t), a = {
      operation: "create",
      data: s,
      options: r
    };
    return this.fetchWithError(i, {
      method: "POST",
      headers: this.baseHeaders(),
      body: JSON.stringify(a)
    });
  }
  async update(e, t, s, r, i) {
    const a = typeof r == "number" || typeof r == "string" ? String(r) : void 0, c = this.buildUrl(e, t, a), o = {
      operation: "update",
      id: Array.isArray(r) ? r : void 0,
      data: s,
      options: i
    };
    return this.fetchWithError(c, {
      method: "POST",
      headers: this.baseHeaders(),
      body: JSON.stringify(o)
    });
  }
  async delete(e, t, s) {
    const r = this.buildUrl(e, t, String(s)), i = {
      operation: "delete"
    };
    return this.fetchWithError(r, {
      method: "POST",
      headers: this.baseHeaders(),
      body: JSON.stringify(i)
    });
  }
}
const f = /* @__PURE__ */ new Map();
function _(n) {
  const e = n.url;
  let t = f.get(e);
  return t || (t = new w(n), f.set(e, t)), t;
}
class w {
  constructor(e) {
    this.ws = null, this.messageHandlers = /* @__PURE__ */ new Map(), this.subscriptions = /* @__PURE__ */ new Map(), this.eventListeners = {}, this.state = "disconnected", this.reconnectAttempts = 0, this.reconnectTimer = null, this.heartbeatTimer = null, this.isManualClose = !1, this.config = {
      url: e.url,
      reconnect: e.reconnect ?? !0,
      reconnectInterval: e.reconnectInterval ?? 3e3,
      maxReconnectAttempts: e.maxReconnectAttempts ?? 10,
      heartbeatInterval: e.heartbeatInterval ?? 3e4,
      debug: e.debug ?? !1
    };
  }
  async connect() {
    if (this.ws?.readyState === WebSocket.OPEN) {
      this.log("Already connected");
      return;
    }
    return this.isManualClose = !1, this.setState("connecting"), new Promise((e, t) => {
      try {
        this.ws = new WebSocket(this.config.url), this.ws.onopen = () => {
          this.log("Connected to WebSocket server"), this.setState("connected"), this.reconnectAttempts = 0, this.startHeartbeat(), this.emit("connect"), e();
        }, this.ws.onmessage = (s) => {
          this.handleMessage(s.data);
        }, this.ws.onerror = (s) => {
          this.log("WebSocket error:", s);
          const r = new Error("WebSocket connection error");
          this.emit("error", r), t(r);
        }, this.ws.onclose = (s) => {
          this.log("WebSocket closed:", s.code, s.reason), this.stopHeartbeat(), this.setState("disconnected"), this.emit("disconnect", s), this.config.reconnect && !this.isManualClose && this.reconnectAttempts < this.config.maxReconnectAttempts && (this.reconnectAttempts++, this.log(`Reconnection attempt ${this.reconnectAttempts}/${this.config.maxReconnectAttempts}`), this.setState("reconnecting"), this.reconnectTimer = setTimeout(() => {
            this.connect().catch((r) => {
              this.log("Reconnection failed:", r);
            });
          }, this.config.reconnectInterval));
        };
      } catch (s) {
        t(s);
      }
    });
  }
  disconnect() {
    this.isManualClose = !0, this.reconnectTimer && (clearTimeout(this.reconnectTimer), this.reconnectTimer = null), this.stopHeartbeat(), this.ws && (this.setState("disconnecting"), this.ws.close(), this.ws = null), this.setState("disconnected"), this.messageHandlers.clear();
  }
  async request(e, t, s) {
    this.ensureConnected();
    const r = l(), i = {
      id: r,
      type: "request",
      operation: e,
      entity: t,
      schema: s?.schema,
      record_id: s?.record_id,
      data: s?.data,
      options: s?.options
    };
    return new Promise((a, c) => {
      this.messageHandlers.set(r, (o) => {
        o.success ? a(o.data) : c(new Error(o.error?.message || "Request failed"));
      }), this.send(i), setTimeout(() => {
        this.messageHandlers.has(r) && (this.messageHandlers.delete(r), c(new Error("Request timeout")));
      }, 3e4);
    });
  }
  async read(e, t) {
    return this.request("read", e, {
      schema: t?.schema,
      record_id: t?.record_id,
      options: {
        filters: t?.filters,
        columns: t?.columns,
        sort: t?.sort,
        preload: t?.preload,
        limit: t?.limit,
        offset: t?.offset
      }
    });
  }
  async create(e, t, s) {
    return this.request("create", e, {
      schema: s?.schema,
      data: t
    });
  }
  async update(e, t, s, r) {
    return this.request("update", e, {
      schema: r?.schema,
      record_id: t,
      data: s
    });
  }
  async delete(e, t, s) {
    await this.request("delete", e, {
      schema: s?.schema,
      record_id: t
    });
  }
  async meta(e, t) {
    return this.request("meta", e, {
      schema: t?.schema
    });
  }
  async subscribe(e, t, s) {
    this.ensureConnected();
    const r = l(), i = {
      id: r,
      type: "subscription",
      operation: "subscribe",
      entity: e,
      schema: s?.schema,
      options: {
        filters: s?.filters
      }
    };
    return new Promise((a, c) => {
      this.messageHandlers.set(r, (o) => {
        if (o.success && o.data?.subscription_id) {
          const h = o.data.subscription_id;
          this.subscriptions.set(h, {
            id: h,
            entity: e,
            schema: s?.schema,
            options: { filters: s?.filters },
            callback: t
          }), this.log(`Subscribed to ${e} with ID: ${h}`), a(h);
        } else
          c(new Error(o.error?.message || "Subscription failed"));
      }), this.send(i), setTimeout(() => {
        this.messageHandlers.has(r) && (this.messageHandlers.delete(r), c(new Error("Subscription timeout")));
      }, 1e4);
    });
  }
  async unsubscribe(e) {
    this.ensureConnected();
    const t = l(), s = {
      id: t,
      type: "subscription",
      operation: "unsubscribe",
      subscription_id: e
    };
    return new Promise((r, i) => {
      this.messageHandlers.set(t, (a) => {
        a.success ? (this.subscriptions.delete(e), this.log(`Unsubscribed from ${e}`), r()) : i(new Error(a.error?.message || "Unsubscribe failed"));
      }), this.send(s), setTimeout(() => {
        this.messageHandlers.has(t) && (this.messageHandlers.delete(t), i(new Error("Unsubscribe timeout")));
      }, 1e4);
    });
  }
  getSubscriptions() {
    return Array.from(this.subscriptions.values());
  }
  getState() {
    return this.state;
  }
  isConnected() {
    return this.ws?.readyState === WebSocket.OPEN;
  }
  on(e, t) {
    this.eventListeners[e] = t;
  }
  off(e) {
    delete this.eventListeners[e];
  }
  // Private methods
  handleMessage(e) {
    try {
      const t = JSON.parse(e);
      switch (this.log("Received message:", t), this.emit("message", t), t.type) {
        case "response":
          this.handleResponse(t);
          break;
        case "notification":
          this.handleNotification(t);
          break;
        case "pong":
          break;
        default:
          this.log("Unknown message type:", t.type);
      }
    } catch (t) {
      this.log("Error parsing message:", t);
    }
  }
  handleResponse(e) {
    const t = this.messageHandlers.get(e.id);
    t && (t(e), this.messageHandlers.delete(e.id));
  }
  handleNotification(e) {
    const t = this.subscriptions.get(e.subscription_id);
    t?.callback && t.callback(e);
  }
  send(e) {
    if (!this.ws || this.ws.readyState !== WebSocket.OPEN)
      throw new Error("WebSocket is not connected");
    const t = JSON.stringify(e);
    this.log("Sending message:", e), this.ws.send(t);
  }
  startHeartbeat() {
    this.heartbeatTimer || (this.heartbeatTimer = setInterval(() => {
      if (this.isConnected()) {
        const e = {
          id: l(),
          type: "ping"
        };
        this.send(e);
      }
    }, this.config.heartbeatInterval));
  }
  stopHeartbeat() {
    this.heartbeatTimer && (clearInterval(this.heartbeatTimer), this.heartbeatTimer = null);
  }
  setState(e) {
    this.state !== e && (this.state = e, this.emit("stateChange", e));
  }
  ensureConnected() {
    if (!this.isConnected())
      throw new Error("WebSocket is not connected. Call connect() first.");
  }
  emit(e, ...t) {
    const s = this.eventListeners[e];
    s && s(...t);
  }
  log(...e) {
    this.config.debug && console.log("[WebSocketClient]", ...e);
  }
}
function C(n) {
  return typeof btoa == "function" ? "ZIP_" + btoa(n) : "ZIP_" + Buffer.from(n, "utf-8").toString("base64");
}
function y(n) {
  let e = n;
  return e.startsWith("ZIP_") ? (e = e.slice(4).replace(/[\n\r ]/g, ""), e = m(e)) : e.startsWith("__") && (e = e.slice(2).replace(/[\n\r ]/g, ""), e = m(e)), (e.startsWith("ZIP_") || e.startsWith("__")) && (e = y(e)), e;
}
function m(n) {
  return typeof atob == "function" ? atob(n) : Buffer.from(n, "base64").toString("utf-8");
}
function u(n) {
  const e = {};
  if (n.columns?.length && (e["X-Select-Fields"] = n.columns.join(",")), n.omit_columns?.length && (e["X-Not-Select-Fields"] = n.omit_columns.join(",")), n.filters?.length)
    for (const t of n.filters) {
      const s = t.logic_operator ?? "AND", r = p(t.operator), i = S(t);
      t.operator === "eq" && s === "AND" ? e[`X-FieldFilter-${t.column}`] = i : s === "OR" ? e[`X-SearchOr-${r}-${t.column}`] = i : e[`X-SearchOp-${r}-${t.column}`] = i;
    }
  if (n.sort?.length) {
    const t = n.sort.map((s) => s.direction.toUpperCase() === "DESC" ? `-${s.column}` : `+${s.column}`);
    e["X-Sort"] = t.join(",");
  }
  if (n.limit !== void 0 && (e["X-Limit"] = String(n.limit)), n.offset !== void 0 && (e["X-Offset"] = String(n.offset)), n.cursor_forward && (e["X-Cursor-Forward"] = n.cursor_forward), n.cursor_backward && (e["X-Cursor-Backward"] = n.cursor_backward), n.preload?.length) {
    const t = n.preload.map((s) => s.columns?.length ? `${s.relation}:${s.columns.join(",")}` : s.relation);
    e["X-Preload"] = t.join("|");
  }
  if (n.fetch_row_number && (e["X-Fetch-RowNumber"] = n.fetch_row_number), n.computedColumns?.length)
    for (const t of n.computedColumns)
      e[`X-CQL-SEL-${t.name}`] = t.expression;
  if (n.customOperators?.length) {
    const t = n.customOperators.map((s) => s.sql);
    e["X-Custom-SQL-W"] = t.join(" AND ");
  }
  return e;
}
function p(n) {
  switch (n) {
    case "eq":
      return "equals";
    case "neq":
      return "notequals";
    case "gt":
      return "greaterthan";
    case "gte":
      return "greaterthanorequal";
    case "lt":
      return "lessthan";
    case "lte":
      return "lessthanorequal";
    case "like":
    case "ilike":
    case "contains":
      return "contains";
    case "startswith":
      return "beginswith";
    case "endswith":
      return "endswith";
    case "in":
      return "in";
    case "between":
      return "between";
    case "between_inclusive":
      return "betweeninclusive";
    case "is_null":
      return "empty";
    case "is_not_null":
      return "notempty";
    default:
      return n;
  }
}
function S(n) {
  return n.value === null || n.value === void 0 ? "" : Array.isArray(n.value) ? n.value.join(",") : String(n.value);
}
const b = /* @__PURE__ */ new Map();
function v(n) {
  const e = n.baseUrl;
  let t = b.get(e);
  return t || (t = new H(n), b.set(e, t)), t;
}
class H {
  constructor(e) {
    this.config = e;
  }
  buildUrl(e, t, s) {
    let r = `${this.config.baseUrl}/${e}/${t}`;
    return s && (r += `/${s}`), r;
  }
  baseHeaders() {
    const e = {
      "Content-Type": "application/json"
    };
    return this.config.token && (e.Authorization = `Bearer ${this.config.token}`), e;
  }
  async fetchWithError(e, t) {
    const s = await fetch(e, t), r = await s.json();
    if (!s.ok)
      throw new Error(r.error?.message || "An error occurred");
    return r;
  }
  async read(e, t, s, r) {
    const i = this.buildUrl(e, t, s), a = r ? u(r) : {};
    return this.fetchWithError(i, {
      method: "GET",
      headers: { ...this.baseHeaders(), ...a }
    });
  }
  async create(e, t, s, r) {
    const i = this.buildUrl(e, t), a = r ? u(r) : {};
    return this.fetchWithError(i, {
      method: "POST",
      headers: { ...this.baseHeaders(), ...a },
      body: JSON.stringify(s)
    });
  }
  async update(e, t, s, r, i) {
    const a = this.buildUrl(e, t, s), c = i ? u(i) : {};
    return this.fetchWithError(a, {
      method: "PUT",
      headers: { ...this.baseHeaders(), ...c },
      body: JSON.stringify(r)
    });
  }
  async delete(e, t, s) {
    const r = this.buildUrl(e, t, s);
    return this.fetchWithError(r, {
      method: "DELETE",
      headers: this.baseHeaders()
    });
  }
}
export {
  H as HeaderSpecClient,
  g as ResolveSpecClient,
  w as WebSocketClient,
  u as buildHeaders,
  y as decodeHeaderValue,
  C as encodeHeaderValue,
  v as getHeaderSpecClient,
  E as getResolveSpecClient,
  _ as getWebSocketClient
};
