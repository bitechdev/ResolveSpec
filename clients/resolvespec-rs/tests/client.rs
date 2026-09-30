use resolvespec::*;
use serde_json::json;
use wiremock::matchers::{header, method, path, query_param};
use wiremock::{Mock, MockServer, ResponseTemplate};

#[tokio::test]
async fn read_posts_body_with_headers() {
    let srv = MockServer::start().await;
    Mock::given(method("POST"))
        .and(path("/public/users"))
        .and(header("authorization", "Bearer tok"))
        .and(header("x-tenant", "a"))
        .respond_with(ResponseTemplate::new(200).set_body_json(json!({"success": true, "data": [{"id": 1}]})))
        .expect(1)
        .mount(&srv)
        .await;
    let c = ResolveSpecClient::from_builder(ClientBuilder::new(&format!("{}/", srv.uri())).token("tok").header("X-Tenant", "a")).unwrap();
    let opts = Options { limit: Some(5), filters: vec![FilterOption::new("a", "eq", 1)], ..Default::default() };
    let r = c.read("public", "users", None, Some(&opts)).await.unwrap();
    let rows: Vec<serde_json::Value> = r.decode().unwrap();
    assert_eq!(rows.len(), 1);
    let body: serde_json::Value = serde_json::from_slice(&srv.received_requests().await.unwrap()[0].body).unwrap();
    assert_eq!(body["operation"], "read");
    assert_eq!(body["options"]["limit"], 5);
    assert!(body.get("id").is_none());
}

#[tokio::test]
async fn id_placement() {
    let srv = MockServer::start().await;
    Mock::given(method("POST")).respond_with(ResponseTemplate::new(200).set_body_json(json!({"success": true, "data": {}}))).mount(&srv).await;
    let c = ResolveSpecClient::new(&srv.uri()).unwrap();
    c.read("s", "e", Some(7.into()), None).await.unwrap();
    c.update("s", "e", json!({"a": 1}), Some(vec!["1".to_string(), "2".to_string()].into()), None).await.unwrap();
    c.delete("s", "e", "a/b").await.unwrap();
    let reqs = srv.received_requests().await.unwrap();
    assert_eq!(reqs[0].url.path(), "/s/e/7");
    assert_eq!(reqs[1].url.path(), "/s/e");
    let b: serde_json::Value = serde_json::from_slice(&reqs[1].body).unwrap();
    assert_eq!(b["id"], json!(["1", "2"]));
    assert_eq!(b["operation"], "update");
    assert_eq!(reqs[2].url.path(), "/s/e/a%2Fb");
}

#[tokio::test]
async fn errors() {
    let srv = MockServer::start().await;
    Mock::given(path("/s/a")).respond_with(ResponseTemplate::new(400).set_body_json(json!({"success": false, "error": {"code": "x", "message": "bad", "detail": "why"}}))).mount(&srv).await;
    Mock::given(path("/s/b")).respond_with(ResponseTemplate::new(502).set_body_string("bad gateway")).mount(&srv).await;
    Mock::given(path("/s/c")).respond_with(ResponseTemplate::new(200).set_body_json(json!({"success": false, "error": {"code": "c", "message": "nope"}}))).mount(&srv).await;
    let c = ResolveSpecClient::new(&srv.uri()).unwrap();
    match c.read("s", "a", None, None).await.unwrap_err() {
        Error::Api { status, message, error } => {
            assert_eq!((status, message.as_str(), error.code.as_str(), error.detail.as_deref()), (400, "bad", "x", Some("why")))
        }
        e => panic!("{e:?}"),
    }
    match c.read("s", "b", None, None).await.unwrap_err() {
        Error::Api { status, message, .. } => assert_eq!((status, message.as_str()), (502, "bad gateway")),
        e => panic!("{e:?}"),
    }
    assert_eq!(c.read("s", "c", None, None).await.unwrap_err().to_string(), "nope");
}

#[test]
fn headers_filters() {
    let o = FuncSpecOptions {
        filters: vec![
            FilterOption::new("status", "eq", "active"),
            FilterOption::new("age", "gte", 18),
            FilterOption::new("name", "contains", "x").or(),
            FilterOption::new("deleted", "is_null", serde_json::Value::Null),
            FilterOption::new("id", "in", json!([1, 2])),
            FilterOption::new("p", "between_inclusive", json!([1, 5])),
        ],
        ..Default::default()
    };
    let h = build_headers(&o);
    let want: std::collections::BTreeMap<String, String> = [
        ("X-FieldFilter-status", "active"),
        ("X-SearchOp-greaterthanorequal-age", "18"),
        ("X-SearchOr-contains-name", "x"),
        ("X-SearchOp-empty-deleted", ""),
        ("X-SearchOp-in-id", "1,2"),
        ("X-SearchOp-betweeninclusive-p", "1,5"),
    ]
    .into_iter()
    .map(|(k, v)| (k.to_string(), v.to_string()))
    .collect();
    assert_eq!(h, want);
}

#[test]
fn headers_misc_and_encoding() {
    let o = FuncSpecOptions {
        search_filters: [("name".to_string(), "bob".to_string())].into(),
        custom_sql_where: Some("a = 1".into()),
        custom_sql_or: Some("b = 2".into()),
        sort: vec![SortOption::new("name", "asc"), SortOption::new("created_at", "DESC")],
        limit: Some(5),
        offset: Some(10),
        distinct: Some(true),
        skip_count: Some(true),
        skip_cache: Some(false),
        response_format: Some("syncfusion".into()),
        ..Default::default()
    };
    let h = build_headers(&o);
    assert_eq!(h["X-Sort"], "name ASC,created_at DESC");
    assert_eq!(h["X-SearchFilter-name"], "bob");
    assert_eq!(h["X-Custom-SQL-W"], "a = 1");
    assert_eq!(h["X-Limit"], "5");
    assert_eq!(h["X-SkipCache"], "false");
    assert_eq!(h["X-Syncfusion"], "true");

    let o = FuncSpecOptions { filters: vec![FilterOption::new("n", "eq", "héllo"), FilterOption::new("m", "eq", " pad")], ..Default::default() };
    let h = build_headers(&o);
    assert!(h["X-FieldFilter-n"].starts_with("ZIP_"));
    assert_eq!(decode_header_value(&h["X-FieldFilter-n"]), "héllo");
    assert_eq!(decode_header_value(&h["X-FieldFilter-m"]), " pad");
}

#[test]
fn query_building() {
    let mut p = Params::new();
    p.insert("a".into(), true.into());
    p.insert("b".into(), vec!["x".to_string(), "y".to_string()].into());
    p.insert("d".into(), 3i64.into());
    assert_eq!(build_query(&p), vec![("a".into(), "true".into()), ("b".into(), "x".into()), ("b".into(), "y".into()), ("d".into(), "3".into())]);
}

#[tokio::test]
async fn query_list_metadata() {
    let srv = MockServer::start().await;
    Mock::given(method("GET"))
        .and(path("/api/users"))
        .and(query_param("org", "1"))
        .and(header("x-limit", "2"))
        .respond_with(ResponseTemplate::new(206).insert_header("Content-Range", "items 10-12/50").set_body_json(json!([{"id": 1}, {"id": 2}])))
        .expect(1)
        .mount(&srv)
        .await;
    let c = FuncSpecClient::from_builder(ClientBuilder::new(&srv.uri()).token("tok")).unwrap();
    let mut p = Params::new();
    p.insert("org".into(), 1i64.into());
    let r = c.query_list("/api/users", &p, Some(&FuncSpecOptions { limit: Some(2), ..Default::default() })).await.unwrap();
    assert_eq!(r.metadata.unwrap(), Metadata { total: 50, count: 2, filtered: 50, limit: 2, offset: 10 });
    assert_eq!(r.data.as_array().unwrap().len(), 2);
}

#[tokio::test]
async fn query_single_and_error() {
    let srv = MockServer::start().await;
    Mock::given(path("/api/ok")).respond_with(ResponseTemplate::new(200).set_body_json(json!({"id": 1}))).mount(&srv).await;
    Mock::given(path("/api/bad")).respond_with(ResponseTemplate::new(400).set_body_json(json!({"success": false, "error": {"code": "hook_error", "message": "Hook execution failed", "detail": "authentication required"}}))).mount(&srv).await;
    let c = FuncSpecClient::new(&srv.uri()).unwrap();
    let r = c.query("api/ok", &Params::new(), None).await.unwrap();
    assert!(r.metadata.is_none());
    assert_eq!(r.data["id"], 1);
    match c.query("api/bad", &Params::new(), None).await.unwrap_err() {
        Error::Api { error, .. } => assert_eq!((error.code.as_str(), error.detail.as_deref()), ("hook_error", Some("authentication required"))),
        e => panic!("{e:?}"),
    }
}
