import { v4 as e } from "uuid";
import { b64DecodeUnicode as t, b64EncodeUnicode as n } from "@warkypublic/artemis-kit/base64";
//#region src/common/http.ts
function r(...e) {
	let t = {};
	for (let n of e) for (let [e, r] of Object.entries(n)) {
		for (let n of Object.keys(t)) n.toLowerCase() === e.toLowerCase() && delete t[n];
		Object.defineProperty(t, e, {
			value: r,
			enumerable: !0,
			configurable: !0,
			writable: !0
		});
	}
	return t;
}
function i(e) {
	return r({ "Content-Type": "application/json" }, e.headers ?? {}, e.token ? { Authorization: `Bearer ${e.token}` } : {});
}
function a(e) {
	let t = Object.entries(i(e)).map(([e, t]) => [e.toLowerCase(), t]).sort(([e], [t]) => e.localeCompare(t));
	return JSON.stringify([e.baseUrl, t]);
}
//#endregion
//#region src/resolvespec/client.ts
var o = /* @__PURE__ */ new Map();
function s(e) {
	let t = a(e), n = o.get(t);
	return n || (n = new c(e), o.set(t, n)), n;
}
var c = class {
	constructor(e) {
		this.config = {
			...e,
			headers: { ...e.headers }
		};
	}
	buildUrl(e, t, n) {
		let r = `${this.config.baseUrl}/${e}/${t}`;
		return n && (r += `/${n}`), r;
	}
	baseHeaders() {
		return i(this.config);
	}
	async fetchWithError(e, t) {
		let n = await fetch(e, t), r = await n.json();
		if (!n.ok) throw Error(r.error?.message || "An error occurred");
		return r;
	}
	async getMetadata(e, t) {
		let n = this.buildUrl(e, t);
		return this.fetchWithError(n, {
			method: "GET",
			headers: this.baseHeaders()
		});
	}
	async read(e, t, n, r) {
		let i = typeof n == "number" || typeof n == "string" ? String(n) : void 0, a = this.buildUrl(e, t, i), o = {
			operation: "read",
			id: Array.isArray(n) ? n : void 0,
			options: r
		};
		return this.fetchWithError(a, {
			method: "POST",
			headers: this.baseHeaders(),
			body: JSON.stringify(o)
		});
	}
	async create(e, t, n, r) {
		let i = this.buildUrl(e, t), a = {
			operation: "create",
			data: n,
			options: r
		};
		return this.fetchWithError(i, {
			method: "POST",
			headers: this.baseHeaders(),
			body: JSON.stringify(a)
		});
	}
	async update(e, t, n, r, i) {
		let a = typeof r == "number" || typeof r == "string" ? String(r) : void 0, o = this.buildUrl(e, t, a), s = {
			operation: "update",
			id: Array.isArray(r) ? r : void 0,
			data: n,
			options: i
		};
		return this.fetchWithError(o, {
			method: "POST",
			headers: this.baseHeaders(),
			body: JSON.stringify(s)
		});
	}
	async delete(e, t, n) {
		let r = this.buildUrl(e, t, String(n));
		return this.fetchWithError(r, {
			method: "POST",
			headers: this.baseHeaders(),
			body: JSON.stringify({ operation: "delete" })
		});
	}
}, l = /* @__PURE__ */ new Map();
function u(e) {
	let t = e.url, n = l.get(t);
	return n || (n = new d(e), l.set(t, n)), n;
}
var d = class {
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
				}, this.ws.onmessage = (e) => {
					this.handleMessage(e.data);
				}, this.ws.onerror = (e) => {
					this.log("WebSocket error:", e);
					let n = /* @__PURE__ */ Error("WebSocket connection error");
					this.emit("error", n), t(n);
				}, this.ws.onclose = (e) => {
					this.log("WebSocket closed:", e.code, e.reason), this.stopHeartbeat(), this.setState("disconnected"), this.emit("disconnect", e), this.config.reconnect && !this.isManualClose && this.reconnectAttempts < this.config.maxReconnectAttempts && (this.reconnectAttempts++, this.log(`Reconnection attempt ${this.reconnectAttempts}/${this.config.maxReconnectAttempts}`), this.setState("reconnecting"), this.reconnectTimer = setTimeout(() => {
						this.connect().catch((e) => {
							this.log("Reconnection failed:", e);
						});
					}, this.config.reconnectInterval));
				};
			} catch (e) {
				t(e);
			}
		});
	}
	disconnect() {
		this.isManualClose = !0, this.reconnectTimer &&= (clearTimeout(this.reconnectTimer), null), this.stopHeartbeat(), this.ws &&= (this.setState("disconnecting"), this.ws.close(), null), this.setState("disconnected"), this.messageHandlers.clear();
	}
	async request(t, n, r) {
		this.ensureConnected();
		let i = e(), a = {
			id: i,
			type: "request",
			operation: t,
			entity: n,
			schema: r?.schema,
			record_id: r?.record_id,
			data: r?.data,
			options: r?.options
		};
		return new Promise((e, t) => {
			this.messageHandlers.set(i, (n) => {
				n.success ? e(n.data) : t(Error(n.error?.message || "Request failed"));
			}), this.send(a), setTimeout(() => {
				this.messageHandlers.has(i) && (this.messageHandlers.delete(i), t(/* @__PURE__ */ Error("Request timeout")));
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
	async create(e, t, n) {
		return this.request("create", e, {
			schema: n?.schema,
			data: t
		});
	}
	async update(e, t, n, r) {
		return this.request("update", e, {
			schema: r?.schema,
			record_id: t,
			data: n
		});
	}
	async delete(e, t, n) {
		await this.request("delete", e, {
			schema: n?.schema,
			record_id: t
		});
	}
	async meta(e, t) {
		return this.request("meta", e, { schema: t?.schema });
	}
	async subscribe(t, n, r) {
		this.ensureConnected();
		let i = e(), a = {
			id: i,
			type: "subscription",
			operation: "subscribe",
			entity: t,
			schema: r?.schema,
			options: { filters: r?.filters }
		};
		return new Promise((e, o) => {
			this.messageHandlers.set(i, (i) => {
				if (i.success && i.data?.subscription_id) {
					let a = i.data.subscription_id;
					this.subscriptions.set(a, {
						id: a,
						entity: t,
						schema: r?.schema,
						options: { filters: r?.filters },
						callback: n
					}), this.log(`Subscribed to ${t} with ID: ${a}`), e(a);
				} else o(Error(i.error?.message || "Subscription failed"));
			}), this.send(a), setTimeout(() => {
				this.messageHandlers.has(i) && (this.messageHandlers.delete(i), o(/* @__PURE__ */ Error("Subscription timeout")));
			}, 1e4);
		});
	}
	async unsubscribe(t) {
		this.ensureConnected();
		let n = e(), r = {
			id: n,
			type: "subscription",
			operation: "unsubscribe",
			subscription_id: t
		};
		return new Promise((e, i) => {
			this.messageHandlers.set(n, (n) => {
				n.success ? (this.subscriptions.delete(t), this.log(`Unsubscribed from ${t}`), e()) : i(Error(n.error?.message || "Unsubscribe failed"));
			}), this.send(r), setTimeout(() => {
				this.messageHandlers.has(n) && (this.messageHandlers.delete(n), i(/* @__PURE__ */ Error("Unsubscribe timeout")));
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
	handleMessage(e) {
		try {
			let t = JSON.parse(e);
			switch (this.log("Received message:", t), this.emit("message", t), t.type) {
				case "response":
					this.handleResponse(t);
					break;
				case "notification":
					this.handleNotification(t);
					break;
				case "pong": break;
				default: this.log("Unknown message type:", t.type);
			}
		} catch (e) {
			this.log("Error parsing message:", e);
		}
	}
	handleResponse(e) {
		let t = this.messageHandlers.get(e.id);
		t && (t(e), this.messageHandlers.delete(e.id));
	}
	handleNotification(e) {
		let t = this.subscriptions.get(e.subscription_id);
		t?.callback && t.callback(e);
	}
	send(e) {
		if (!this.ws || this.ws.readyState !== WebSocket.OPEN) throw Error("WebSocket is not connected");
		let t = JSON.stringify(e);
		this.log("Sending message:", e), this.ws.send(t);
	}
	startHeartbeat() {
		this.heartbeatTimer ||= setInterval(() => {
			if (this.isConnected()) {
				let t = {
					id: e(),
					type: "ping"
				};
				this.send(t);
			}
		}, this.config.heartbeatInterval);
	}
	stopHeartbeat() {
		this.heartbeatTimer &&= (clearInterval(this.heartbeatTimer), null);
	}
	setState(e) {
		this.state !== e && (this.state = e, this.emit("stateChange", e));
	}
	ensureConnected() {
		if (!this.isConnected()) throw Error("WebSocket is not connected. Call connect() first.");
	}
	emit(e, ...t) {
		let n = this.eventListeners[e];
		n && n(...t);
	}
	log(...e) {
		this.config.debug && console.log("[WebSocketClient]", ...e);
	}
};
//#endregion
//#region src/headerspec/client.ts
function f(e) {
	return "ZIP_" + n(e);
}
function p(e) {
	let t = e;
	return t.startsWith("ZIP_") ? (t = t.slice(4).replace(/[\n\r ]/g, ""), t = m(t)) : t.startsWith("__") && (t = t.slice(2).replace(/[\n\r ]/g, ""), t = m(t)), (t.startsWith("ZIP_") || t.startsWith("__")) && (t = p(t)), t;
}
function m(e) {
	return t(e);
}
function h(e) {
	let t = {};
	if (e.columns?.length && (t["X-Select-Fields"] = e.columns.join(",")), e.omit_columns?.length && (t["X-Not-Select-Fields"] = e.omit_columns.join(",")), e.filters?.length) for (let n of e.filters) {
		let e = n.logic_operator ?? "AND", r = g(n.operator), i = _(n);
		n.operator === "eq" && e === "AND" ? t[`X-FieldFilter-${n.column}`] = i : e === "OR" ? t[`X-SearchOr-${r}-${n.column}`] = i : t[`X-SearchOp-${r}-${n.column}`] = i;
	}
	if (e.sort?.length && (t["X-Sort"] = e.sort.map((e) => e.direction.toUpperCase() === "DESC" ? `-${e.column}` : `+${e.column}`).join(",")), e.limit !== void 0 && (t["X-Limit"] = String(e.limit)), e.offset !== void 0 && (t["X-Offset"] = String(e.offset)), e.cursor_forward && (t["X-Cursor-Forward"] = e.cursor_forward), e.cursor_backward && (t["X-Cursor-Backward"] = e.cursor_backward), e.preload?.length && (t["X-Preload"] = e.preload.map((e) => e.columns?.length ? `${e.relation}:${e.columns.join(",")}` : e.relation).join("|")), e.fetch_row_number && (t["X-Fetch-RowNumber"] = e.fetch_row_number), e.computedColumns?.length) for (let n of e.computedColumns) t[`X-CQL-SEL-${n.name}`] = n.expression;
	return e.customOperators?.length && (t["X-Custom-SQL-W"] = e.customOperators.map((e) => e.sql).join(" AND ")), t;
}
function g(e) {
	switch (e) {
		case "eq": return "equals";
		case "neq": return "notequals";
		case "gt": return "greaterthan";
		case "gte": return "greaterthanorequal";
		case "lt": return "lessthan";
		case "lte": return "lessthanorequal";
		case "like":
		case "ilike":
		case "contains": return "contains";
		case "startswith": return "beginswith";
		case "endswith": return "endswith";
		case "in": return "in";
		case "between": return "between";
		case "between_inclusive": return "betweeninclusive";
		case "is_null": return "empty";
		case "is_not_null": return "notempty";
		default: return e;
	}
}
function _(e) {
	return e.value === null || e.value === void 0 ? "" : Array.isArray(e.value) ? e.value.join(",") : String(e.value);
}
var v = /* @__PURE__ */ new Map();
function y(e) {
	let t = a(e), n = v.get(t);
	return n || (n = new b(e), v.set(t, n)), n;
}
var b = class {
	constructor(e) {
		this.config = {
			...e,
			headers: { ...e.headers }
		};
	}
	buildUrl(e, t, n) {
		let r = `${this.config.baseUrl}/${e}/${t}`;
		return n && (r += `/${n}`), r;
	}
	baseHeaders() {
		return i(this.config);
	}
	async fetchWithError(e, t) {
		let n = await fetch(e, t), r = await n.json();
		if (!n.ok) throw Error(r.error?.message || `${n.statusText} (${n.status})`);
		return {
			data: r,
			success: !0,
			error: r.error ? r.error : void 0,
			metadata: {
				count: n.headers.get("content-range") ? Number(n.headers.get("content-range")?.split("/")[1]) : 0,
				total: n.headers.get("content-range") ? Number(n.headers.get("content-range")?.split("/")[1]) : 0,
				filtered: n.headers.get("content-range") ? Number(n.headers.get("content-range")?.split("/")[1]) : 0,
				offset: n.headers.get("content-range") ? Number(n.headers.get("content-range")?.split("/")[0].split("-")[0]) : 0,
				limit: n.headers.get("x-limit") ? Number(n.headers.get("x-limit")) : 0
			}
		};
	}
	async read(e, t, n, i) {
		let a = this.buildUrl(e, t, n), o = i ? h(i) : {};
		return this.fetchWithError(a, {
			method: "GET",
			headers: r(this.baseHeaders(), o)
		});
	}
	async create(e, t, n, i) {
		let a = this.buildUrl(e, t), o = i ? h(i) : {};
		return this.fetchWithError(a, {
			method: "POST",
			headers: r(this.baseHeaders(), o),
			body: JSON.stringify(n)
		});
	}
	async update(e, t, n, i, a) {
		let o = this.buildUrl(e, t, n), s = a ? h(a) : {};
		return this.fetchWithError(o, {
			method: "PUT",
			headers: r(this.baseHeaders(), s),
			body: JSON.stringify(i)
		});
	}
	async delete(e, t, n) {
		let r = this.buildUrl(e, t, n);
		return this.fetchWithError(r, {
			method: "DELETE",
			headers: this.baseHeaders()
		});
	}
};
//#endregion
export { b as HeaderSpecClient, c as ResolveSpecClient, d as WebSocketClient, h as buildHeaders, p as decodeHeaderValue, f as encodeHeaderValue, y as getHeaderSpecClient, s as getResolveSpecClient, u as getWebSocketClient };
