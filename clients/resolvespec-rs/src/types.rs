//! Types aligned with Go `pkg/common/types.go`. Field names are the wire names.
use serde::{Deserialize, Serialize};
use serde_json::Value;
use std::collections::HashMap;

#[derive(Debug, Clone, Default, Serialize, Deserialize)]
pub struct FilterOption {
    pub column: String,
    /// eq neq gt gte lt lte like ilike in contains startswith endswith between
    /// between_inclusive is_null is_not_null
    pub operator: String,
    #[serde(default)]
    pub value: Value,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub logic_operator: Option<String>, // AND | OR
}

impl FilterOption {
    pub fn new(column: &str, operator: &str, value: impl Into<Value>) -> Self {
        Self { column: column.into(), operator: operator.into(), value: value.into(), logic_operator: None }
    }
    pub fn or(mut self) -> Self {
        self.logic_operator = Some("OR".into());
        self
    }
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct SortOption {
    pub column: String,
    pub direction: String, // asc | desc
}

impl SortOption {
    pub fn new(column: &str, direction: &str) -> Self {
        Self { column: column.into(), direction: direction.into() }
    }
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Parameter {
    pub name: String,
    pub value: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub sequence: Option<i32>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct CustomOperator {
    pub name: String,
    pub sql: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ComputedColumn {
    pub name: String,
    pub expression: String,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize)]
pub struct PreloadOption {
    #[serde(skip_serializing_if = "Option::is_none")]
    pub relation: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub table_name: Option<String>,
    #[serde(skip_serializing_if = "Vec::is_empty", default)]
    pub columns: Vec<String>,
    #[serde(skip_serializing_if = "Vec::is_empty", default)]
    pub omit_columns: Vec<String>,
    #[serde(skip_serializing_if = "Vec::is_empty", default)]
    pub sort: Vec<SortOption>,
    #[serde(skip_serializing_if = "Vec::is_empty", default)]
    pub filters: Vec<FilterOption>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub r#where: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub limit: Option<i64>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub offset: Option<i64>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub updateable: Option<bool>,
    #[serde(skip_serializing_if = "HashMap::is_empty", default)]
    pub computed_ql: HashMap<String, String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub recursive: Option<bool>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub primary_key: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub related_key: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub foreign_key: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub recursive_child_key: Option<String>,
    #[serde(skip_serializing_if = "Vec::is_empty", default)]
    pub sql_joins: Vec<String>,
    #[serde(skip_serializing_if = "Vec::is_empty", default)]
    pub join_aliases: Vec<String>,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize)]
pub struct VectorSearchOption {
    pub column: String,
    pub vector: Vec<f64>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub metric: Option<String>, // l2 (default) | cosine | ip
    #[serde(rename = "as", skip_serializing_if = "Option::is_none")]
    pub alias: Option<String>, // distance alias, default _distance
    #[serde(skip_serializing_if = "Option::is_none")]
    pub direction: Option<String>,
}

/// ResolveSpec request options object.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
pub struct Options {
    #[serde(skip_serializing_if = "Vec::is_empty", default)]
    pub preload: Vec<PreloadOption>,
    #[serde(skip_serializing_if = "Vec::is_empty", default)]
    pub columns: Vec<String>,
    #[serde(skip_serializing_if = "Vec::is_empty", default)]
    pub omit_columns: Vec<String>,
    #[serde(skip_serializing_if = "Vec::is_empty", default)]
    pub filters: Vec<FilterOption>,
    #[serde(skip_serializing_if = "Vec::is_empty", default)]
    pub sort: Vec<SortOption>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub limit: Option<i64>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub offset: Option<i64>,
    #[serde(rename = "customOperators", skip_serializing_if = "Vec::is_empty", default)]
    pub custom_operators: Vec<CustomOperator>,
    #[serde(rename = "computedColumns", skip_serializing_if = "Vec::is_empty", default)]
    pub computed_columns: Vec<ComputedColumn>,
    #[serde(skip_serializing_if = "Vec::is_empty", default)]
    pub parameters: Vec<Parameter>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub cursor_forward: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub cursor_backward: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub fetch_row_number: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub vector_search: Option<VectorSearchOption>,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq, Eq)]
pub struct Metadata {
    #[serde(default)]
    pub total: i64,
    #[serde(default)]
    pub count: i64,
    #[serde(default)]
    pub filtered: i64,
    #[serde(default)]
    pub limit: i64,
    #[serde(default)]
    pub offset: i64,
}

/// ResolveSpec envelope. `data` is left as JSON for the caller to decode.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
pub struct Response {
    #[serde(default)]
    pub success: bool,
    #[serde(default)]
    pub data: Value,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub metadata: Option<Metadata>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub error: Option<ApiError>,
}

impl Response {
    /// Decode `data` into `T`.
    pub fn decode<T: serde::de::DeserializeOwned>(&self) -> Result<T, serde_json::Error> {
        serde_json::from_value(self.data.clone())
    }
}

#[derive(Debug, Clone, Default, Serialize, Deserialize)]
pub struct ApiError {
    #[serde(default)]
    pub code: String,
    #[serde(default)]
    pub message: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub details: Option<Value>,
    /// Server-side reason (funcspec / restheadspec).
    #[serde(skip_serializing_if = "Option::is_none")]
    pub detail: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub sql: Option<String>,
}
