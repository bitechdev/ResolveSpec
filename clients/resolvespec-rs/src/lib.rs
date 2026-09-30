//! Client for ResolveSpec (JSON body) and FunctionSpec endpoints.
mod client;
mod error;
mod funcspec;
mod resolvespec;
pub mod types;

pub use client::ClientBuilder;
pub use error::{Error, Result};
pub use funcspec::{build_headers, build_query, decode_header_value, encode_header_value, FuncSpecClient, FuncSpecOptions, Param, Params};
pub use resolvespec::{RecordId, ResolveSpecClient};
pub use types::*;
