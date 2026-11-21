//! The plumbing: where this bog listens and keeps its data, how handlers
//! reach bog, how errors answer, and how it stops. A new route or view needs
//! no change here: see schema.rs and api.rs.

mod api;
mod schema;

use std::sync::Arc;

use aide::openapi::{Info, OpenApi};
use axum::Json;
use axum::extract::State;
use axum::http::StatusCode;
use axum::response::{IntoResponse, Response};
use axum::routing::get;
use bog::schema::Db;
use schema::App;

/// The database, shared by every handler.
pub type Bog = Arc<Db<App>>;

/// Runs a bog call off the async threads. bog's calls block (they read and
/// write the disk), so a handler never calls bog directly: always through
/// this.
pub async fn bog<T: Send + 'static>(
    db: &Bog,
    f: impl FnOnce(&Db<App>) -> bog::Result<T> + Send + 'static,
) -> Result<T, Problem> {
    let db = db.clone();
    tokio::task::spawn_blocking(move || f(&db))
        .await
        .map_err(|e| Problem(StatusCode::INTERNAL_SERVER_ERROR, e.to_string()))?
        .map_err(Problem::from)
}

/// An error as the API answers it: a status, and `{"error": "..."}`.
pub struct Problem(pub StatusCode, pub String);

impl From<bog::Error> for Problem {
    fn from(e: bog::Error) -> Self {
        match &e {
            // a rule of the schema refused the write: the caller's mistake
            bog::Error::Rejected(why) => Problem(StatusCode::UNPROCESSABLE_ENTITY, why.to_string()),
            _ => Problem(StatusCode::SERVICE_UNAVAILABLE, e.to_string()),
        }
    }
}

impl IntoResponse for Problem {
    fn into_response(self) -> Response {
        (self.0, Json(serde_json::json!({ "error": self.1 }))).into_response()
    }
}

// lets a handler that may fail be documented: its Ok side is
impl aide::OperationOutput for Problem {
    type Inner = Self;
}

/// What the platform asks to learn whether this bog is healthy.
async fn healthz(State(db): State<Bog>) -> Result<&'static str, Problem> {
    bog(&db, |db| db.read(schema::check)).await?;
    Ok("ok")
}

/// Resolves when the bog is told to stop: SIGTERM (a deploy, a restart), or
/// ctrl-c.
async fn stop() {
    let mut term =
        tokio::signal::unix::signal(tokio::signal::unix::SignalKind::terminate()).expect("SIGTERM");
    tokio::select! {
        _ = tokio::signal::ctrl_c() => {}
        _ = term.recv() => {}
    }
}

#[tokio::main]
async fn main() -> std::io::Result<()> {
    // deployed, PORT and BOG_DATA_DIR are set; locally: loopback, and ./data
    let addr = match std::env::var("PORT") {
        Ok(port) => format!("0.0.0.0:{port}"),
        Err(_) => "127.0.0.1:7877".into(),
    };
    let dir = std::env::var("BOG_DATA_DIR").unwrap_or_else(|_| "./data".into());
    let db: Bog = Arc::new(App::open(&dir).build().map_err(std::io::Error::other)?);

    let mut doc = OpenApi {
        info: Info {
            title: env!("CARGO_PKG_NAME").into(),
            version: env!("CARGO_PKG_VERSION").into(),
            ..Info::default()
        },
        ..OpenApi::default()
    };
    let routes = api::routes().finish_api(&mut doc);
    let doc = serde_json::to_value(&doc).map_err(std::io::Error::other)?;
    let app = routes
        .route("/openapi.json", get(move || async move { Json(doc) }))
        .route("/healthz", get(healthz))
        .with_state(db.clone());

    eprintln!("{} on http://{addr}, data in {dir}", env!("CARGO_PKG_NAME"));
    let listener = tokio::net::TcpListener::bind(&addr).await?;
    axum::serve(listener, app)
        .with_graceful_shutdown(stop())
        .await?;
    // stopped: write everything down, so the next start has nothing to replay
    tokio::task::spawn_blocking(move || db.checkpoint())
        .await
        .map_err(std::io::Error::other)?
        .map_err(std::io::Error::other)?;
    Ok(())
}
