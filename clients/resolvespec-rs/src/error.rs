use crate::types::ApiError;

/// Returned on transport failure, a non-2xx response or an unsuccessful API result.
#[derive(Debug, thiserror::Error)]
pub enum Error {
    #[error("{message}")]
    Api { status: u16, message: String, error: ApiError },
    #[error(transparent)]
    Http(#[from] reqwest::Error),
    #[error(transparent)]
    Json(#[from] serde_json::Error),
}

pub type Result<T> = std::result::Result<T, Error>;

pub(crate) fn error_from(status: u16, body: &str) -> Error {
    let parsed: Option<serde_json::Value> = serde_json::from_str(body).ok();
    let err: ApiError = parsed
        .as_ref()
        .and_then(|v| v.get("error"))
        .and_then(|e| serde_json::from_value(e.clone()).ok())
        .unwrap_or_default();
    let message = if !err.message.is_empty() {
        err.message.clone()
    } else {
        let text = if parsed.is_none() { body.trim().chars().take(200).collect::<String>() } else { String::new() };
        if text.is_empty() {
            format!("{} ({})", reqwest::StatusCode::from_u16(status).ok().and_then(|s| s.canonical_reason()).unwrap_or("Error"), status)
        } else {
            text
        }
    };
    Error::Api { status, message, error: err }
}
