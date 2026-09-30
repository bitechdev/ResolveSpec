use std::collections::HashMap;
use std::time::Duration;

use reqwest::header::{HeaderMap, HeaderName, HeaderValue, AUTHORIZATION, CONTENT_TYPE};

use crate::error::Result;

/// Shared HTTP configuration.
#[derive(Clone)]
pub(crate) struct Config {
    pub base_url: String,
    pub token: Option<String>,
    pub headers: HashMap<String, String>,
    pub http: reqwest::Client,
}

/// Builder options shared by both clients.
#[derive(Default, Clone)]
pub struct ClientBuilder {
    base_url: String,
    token: Option<String>,
    headers: HashMap<String, String>,
    timeout: Option<Duration>,
    http: Option<reqwest::Client>,
}

impl ClientBuilder {
    pub fn new(base_url: &str) -> Self {
        Self { base_url: base_url.trim_end_matches('/').into(), timeout: Some(Duration::from_secs(30)), ..Default::default() }
    }
    pub fn token(mut self, token: &str) -> Self {
        self.token = Some(token.into());
        self
    }
    pub fn header(mut self, name: &str, value: &str) -> Self {
        self.headers.insert(name.into(), value.into());
        self
    }
    pub fn timeout(mut self, t: Duration) -> Self {
        self.timeout = Some(t);
        self
    }
    pub fn http_client(mut self, c: reqwest::Client) -> Self {
        self.http = Some(c);
        self
    }
    pub(crate) fn config(self) -> Result<Config> {
        let http = match self.http {
            Some(c) => c,
            None => {
                let mut b = reqwest::Client::builder();
                if let Some(t) = self.timeout {
                    b = b.timeout(t);
                }
                b.build()?
            }
        };
        Ok(Config { base_url: self.base_url, token: self.token, headers: self.headers, http })
    }
}

impl Config {
    /// Content-Type < custom headers < extra (per-call) < bearer token.
    pub fn headers(&self, extra: &HashMap<String, String>) -> HeaderMap {
        let mut m = HeaderMap::new();
        m.insert(CONTENT_TYPE, HeaderValue::from_static("application/json"));
        for (k, v) in self.headers.iter().chain(extra.iter()) {
            if let (Ok(n), Ok(v)) = (HeaderName::try_from(k.as_str()), HeaderValue::from_str(v)) {
                m.insert(n, v);
            }
        }
        if let Some(t) = &self.token {
            if let Ok(v) = HeaderValue::from_str(&format!("Bearer {t}")) {
                m.insert(AUTHORIZATION, v);
            }
        }
        m
    }
}

pub(crate) fn path_segment(s: &str) -> String {
    let mut out = String::new();
    for b in s.bytes() {
        match b {
            b'A'..=b'Z' | b'a'..=b'z' | b'0'..=b'9' | b'-' | b'.' | b'_' | b'~' => out.push(b as char),
            _ => out.push_str(&format!("%{b:02X}")),
        }
    }
    out
}
