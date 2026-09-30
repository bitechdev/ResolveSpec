use std::collections::{BTreeMap, HashMap};

use base64::{engine::general_purpose::STANDARD, Engine};
use reqwest::Method;
use serde_json::Value;

use crate::client::{ClientBuilder, Config};
use crate::error::{error_from, Result};
use crate::types::{FilterOption, Metadata, Response, SortOption};

/// Options sent to funcspec endpoints as `X-*` headers.
///
/// Server behaviour (`pkg/funcspec`): `sort` is inserted raw into ORDER BY (so it is sent as SQL
/// terms); only one search operator per column is kept; values starting with `ZIP_` or `__`
/// are base64-decoded by the server, so such plaintext values cannot be sent faithfully.
#[derive(Debug, Clone, Default)]
pub struct FuncSpecOptions {
    /// eq+AND -> X-FieldFilter; others X-SearchOp / X-SearchOr.
    pub filters: Vec<FilterOption>,
    /// X-SearchFilter-{col}: text ILIKE.
    pub search_filters: BTreeMap<String, String>,
    pub custom_sql_where: Option<String>,
    pub custom_sql_or: Option<String>,
    pub sort: Vec<SortOption>,
    pub limit: Option<i64>,
    pub offset: Option<i64>,
    pub distinct: Option<bool>,
    pub skip_count: Option<bool>,
    pub skip_cache: Option<bool>,
    /// simple | detail | syncfusion
    pub response_format: Option<String>,
}

/// Query-string parameter value. `List` is sent as repeated keys (server: IN filter).
#[derive(Debug, Clone)]
pub enum Param {
    Str(String),
    Int(i64),
    Bool(bool),
    List(Vec<String>),
}

impl From<&str> for Param {
    fn from(v: &str) -> Self {
        Self::Str(v.into())
    }
}
impl From<String> for Param {
    fn from(v: String) -> Self {
        Self::Str(v)
    }
}
impl From<i64> for Param {
    fn from(v: i64) -> Self {
        Self::Int(v)
    }
}
impl From<bool> for Param {
    fn from(v: bool) -> Self {
        Self::Bool(v)
    }
}
impl From<Vec<String>> for Param {
    fn from(v: Vec<String>) -> Self {
        Self::List(v)
    }
}

pub type Params = BTreeMap<String, Param>;

fn operator(op: &str) -> &str {
    match op {
        "eq" => "equals",
        "neq" => "notequals",
        "gt" => "greaterthan",
        "gte" => "greaterthanorequal",
        "lt" => "lessthan",
        "lte" => "lessthanorequal",
        "like" | "ilike" | "contains" => "contains",
        "startswith" => "beginswith",
        "endswith" => "endswith",
        "in" => "in",
        "between" => "between",
        "between_inclusive" => "betweeninclusive",
        "is_null" => "empty",
        "is_not_null" => "notempty",
        other => other,
    }
}

fn scalar(v: &Value) -> String {
    match v {
        Value::Null => String::new(),
        Value::String(s) => s.clone(),
        Value::Array(a) => a.iter().map(scalar).collect::<Vec<_>>().join(","),
        other => other.to_string(),
    }
}

/// Base64 (UTF-8) with the `ZIP_` prefix.
pub fn encode_header_value(v: &str) -> String {
    format!("ZIP_{}", STANDARD.encode(v.as_bytes()))
}

/// Decode a value that may carry a `ZIP_` or `__` prefix (nested allowed).
pub fn decode_header_value(v: &str) -> String {
    for p in ["ZIP_", "__"] {
        if let Some(rest) = v.strip_prefix(p) {
            let mut b64: String = rest.chars().filter(|c| !matches!(c, '\n' | '\r' | ' ')).collect();
            while b64.len() % 4 != 0 {
                b64.push('=');
            }
            return match STANDARD.decode(b64).ok().and_then(|b| String::from_utf8(b).ok()) {
                Some(s) => decode_header_value(&s),
                None => v.to_string(),
            };
        }
    }
    v.to_string()
}

/// Encode values that are unsafe as raw header/query text (non-ASCII, control chars, edge spaces).
fn safe(v: &str) -> String {
    if v != v.trim() || v.chars().any(|c| !c.is_ascii() || c.is_ascii_control()) {
        encode_header_value(v)
    } else {
        v.to_string()
    }
}

/// Build the `X-*` headers understood by `funcspec.ParseParameters`.
pub fn build_headers(o: &FuncSpecOptions) -> BTreeMap<String, String> {
    let mut h = BTreeMap::new();
    for f in &o.filters {
        let logic = f.logic_operator.as_deref().unwrap_or("AND");
        let v = safe(&scalar(&f.value));
        if f.operator == "eq" && logic == "AND" {
            h.insert(format!("X-FieldFilter-{}", f.column), v);
        } else {
            let kind = if logic == "OR" { "X-SearchOr" } else { "X-SearchOp" };
            h.insert(format!("{kind}-{}-{}", operator(&f.operator), f.column), v);
        }
    }
    for (col, text) in &o.search_filters {
        h.insert(format!("X-SearchFilter-{col}"), safe(text));
    }
    if let Some(v) = o.custom_sql_where.as_deref().filter(|s| !s.is_empty()) {
        h.insert("X-Custom-SQL-W".into(), safe(v));
    }
    if let Some(v) = o.custom_sql_or.as_deref().filter(|s| !s.is_empty()) {
        h.insert("X-Custom-SQL-Or".into(), safe(v));
    }
    if !o.sort.is_empty() {
        let terms: Vec<String> = o
            .sort
            .iter()
            .map(|s| format!("{} {}", s.column, if s.direction.eq_ignore_ascii_case("desc") { "DESC" } else { "ASC" }))
            .collect();
        h.insert("X-Sort".into(), safe(&terms.join(","))); // funcspec puts this verbatim into ORDER BY
    }
    if let Some(n) = o.limit {
        h.insert("X-Limit".into(), n.to_string());
    }
    if let Some(n) = o.offset {
        h.insert("X-Offset".into(), n.to_string());
    }
    for (name, v) in [("X-Distinct", o.distinct), ("X-SkipCount", o.skip_count), ("X-SkipCache", o.skip_cache)] {
        if let Some(b) = v {
            h.insert(name.into(), b.to_string());
        }
    }
    match o.response_format.as_deref() {
        Some("simple") => h.insert("X-SimpleApi".into(), "true".into()),
        Some("detail") => h.insert("X-DetailApi".into(), "true".into()),
        Some("syncfusion") => h.insert("X-Syncfusion".into(), "true".into()),
        _ => None,
    };
    h
}

/// Build query-string pairs: bools -> true/false, lists -> repeated keys.
pub fn build_query(p: &Params) -> Vec<(String, String)> {
    let mut out = Vec::new();
    for (k, v) in p {
        match v {
            Param::Str(s) => out.push((k.clone(), safe(s))),
            Param::Int(n) => out.push((k.clone(), n.to_string())),
            Param::Bool(b) => out.push((k.clone(), b.to_string())),
            Param::List(l) => out.extend(l.iter().map(|s| (k.clone(), safe(s)))),
        }
    }
    out
}

fn metadata(content_range: Option<&str>, limit: Option<i64>) -> Metadata {
    let mut m = Metadata { limit: limit.unwrap_or(0), ..Default::default() };
    if let Some(cr) = content_range {
        // "items {start}-{end}/{total}"
        let rest = cr.rsplit(' ').next().unwrap_or("");
        if let Some((range, total)) = rest.split_once('/') {
            if let (Some((s, e)), Ok(t)) = (range.split_once('-'), total.parse::<i64>()) {
                if let (Ok(s), Ok(e)) = (s.parse::<i64>(), e.parse::<i64>()) {
                    m.total = t;
                    m.filtered = t;
                    m.count = e - s;
                    m.offset = s;
                }
            }
        }
    }
    m
}

/// Client for user-defined SQL endpoints. Routes are defined by the server application.
#[derive(Clone)]
pub struct FuncSpecClient {
    cfg: Config,
}

impl FuncSpecClient {
    pub fn new(base_url: &str) -> Result<Self> {
        Self::from_builder(ClientBuilder::new(base_url))
    }

    pub fn from_builder(b: ClientBuilder) -> Result<Self> {
        Ok(Self { cfg: b.config()? })
    }

    async fn call(&self, method: Method, path: &str, params: &Params, options: Option<&FuncSpecOptions>, list: bool) -> Result<Response> {
        let url = format!("{}/{}", self.cfg.base_url, path.trim_start_matches('/'));
        let extra: HashMap<String, String> = options.map(|o| build_headers(o).into_iter().collect()).unwrap_or_default();
        let resp = self.cfg.http.request(method, url).headers(self.cfg.headers(&extra)).query(&build_query(params)).send().await?;
        let status = resp.status();
        let cr = resp.headers().get("content-range").and_then(|v| v.to_str().ok()).map(str::to_owned);
        let text = resp.text().await?;
        if !status.is_success() {
            // 206 Partial Content is success
            return Err(error_from(status.as_u16(), &text));
        }
        let data = if text.trim().is_empty() { Value::Null } else { serde_json::from_str(&text)? };
        Ok(Response {
            success: true,
            data,
            metadata: list.then(|| metadata(cr.as_deref(), options.and_then(|o| o.limit))),
            error: None,
        })
    }

    /// Single-record endpoint (`SqlQuery`). `data` is the row object.
    pub async fn query(&self, path: &str, params: &Params, options: Option<&FuncSpecOptions>) -> Result<Response> {
        self.call(Method::GET, path, params, options, false).await
    }

    /// List endpoint (`SqlQueryList`). Metadata comes from Content-Range.
    pub async fn query_list(&self, path: &str, params: &Params, options: Option<&FuncSpecOptions>) -> Result<Response> {
        self.call(Method::GET, path, params, options, true).await
    }

    /// Like `query` / `query_list` with an explicit HTTP method (routes are app-defined).
    pub async fn request(&self, method: Method, path: &str, params: &Params, options: Option<&FuncSpecOptions>, list: bool) -> Result<Response> {
        self.call(method, path, params, options, list).await
    }
}
