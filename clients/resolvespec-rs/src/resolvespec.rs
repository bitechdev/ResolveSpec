use std::collections::HashMap;

use reqwest::Method;
use serde::Serialize;
use serde_json::Value;

use crate::client::{path_segment, ClientBuilder, Config};
use crate::error::{error_from, Error, Result};
use crate::types::{Options, Response};

/// A record id: a single value goes in the URL, a list goes in the body.
#[derive(Debug, Clone)]
pub enum RecordId {
    Int(i64),
    Str(String),
    Many(Vec<String>),
}

impl From<i64> for RecordId {
    fn from(v: i64) -> Self {
        Self::Int(v)
    }
}
impl From<&str> for RecordId {
    fn from(v: &str) -> Self {
        Self::Str(v.into())
    }
}
impl From<Vec<String>> for RecordId {
    fn from(v: Vec<String>) -> Self {
        Self::Many(v)
    }
}

fn url_id(id: &Option<RecordId>) -> Option<String> {
    match id {
        Some(RecordId::Int(n)) => Some(n.to_string()),
        Some(RecordId::Str(s)) => Some(s.clone()),
        _ => None,
    }
}

#[derive(Serialize)]
struct Request<'a> {
    operation: &'a str,
    #[serde(skip_serializing_if = "Option::is_none")]
    id: Option<Vec<String>>,
    #[serde(skip_serializing_if = "Option::is_none")]
    data: Option<Value>,
    #[serde(skip_serializing_if = "Option::is_none")]
    options: Option<&'a Options>,
}

/// Client for the ResolveSpec JSON body protocol.
#[derive(Clone)]
pub struct ResolveSpecClient {
    cfg: Config,
}

impl ResolveSpecClient {
    pub fn new(base_url: &str) -> Result<Self> {
        Self::from_builder(ClientBuilder::new(base_url))
    }

    pub fn from_builder(b: ClientBuilder) -> Result<Self> {
        Ok(Self { cfg: b.config()? })
    }

    fn url(&self, schema: &str, entity: &str, id: Option<String>) -> String {
        let mut u = format!("{}/{}/{}", self.cfg.base_url, path_segment(schema), path_segment(entity));
        if let Some(id) = id.filter(|i| !i.is_empty()) {
            u.push('/');
            u.push_str(&path_segment(&id));
        }
        u
    }

    async fn send(&self, method: Method, url: String, body: Option<Request<'_>>) -> Result<Response> {
        let mut req = self.cfg.http.request(method, url).headers(self.cfg.headers(&HashMap::new()));
        if let Some(b) = body {
            req = req.body(serde_json::to_vec(&b)?);
        }
        let resp = req.send().await?;
        let status = resp.status();
        let text = resp.text().await?;
        if !status.is_success() {
            return Err(error_from(status.as_u16(), &text));
        }
        let out: Response = serde_json::from_str(&text)?;
        if !out.success {
            if let Some(e) = out.error.clone() {
                return Err(Error::Api { status: status.as_u16(), message: e.message.clone(), error: e });
            }
        }
        Ok(out)
    }

    /// GET /{schema}/{entity}
    pub async fn get_metadata(&self, schema: &str, entity: &str) -> Result<Response> {
        self.send(Method::GET, self.url(schema, entity, None), None).await
    }

    pub async fn read(&self, schema: &str, entity: &str, id: Option<RecordId>, options: Option<&Options>) -> Result<Response> {
        let body = Request { operation: "read", id: many(&id), data: None, options };
        self.send(Method::POST, self.url(schema, entity, url_id(&id)), Some(body)).await
    }

    pub async fn create(&self, schema: &str, entity: &str, data: Value, options: Option<&Options>) -> Result<Response> {
        let body = Request { operation: "create", id: None, data: Some(data), options };
        self.send(Method::POST, self.url(schema, entity, None), Some(body)).await
    }

    pub async fn update(&self, schema: &str, entity: &str, data: Value, id: Option<RecordId>, options: Option<&Options>) -> Result<Response> {
        let body = Request { operation: "update", id: many(&id), data: Some(data), options };
        self.send(Method::POST, self.url(schema, entity, url_id(&id)), Some(body)).await
    }

    pub async fn delete(&self, schema: &str, entity: &str, id: impl Into<RecordId>) -> Result<Response> {
        let id = Some(id.into());
        let body = Request { operation: "delete", id: None, data: None, options: None };
        self.send(Method::POST, self.url(schema, entity, url_id(&id)), Some(body)).await
    }
}

fn many(id: &Option<RecordId>) -> Option<Vec<String>> {
    match id {
        Some(RecordId::Many(v)) => Some(v.clone()),
        _ => None,
    }
}
