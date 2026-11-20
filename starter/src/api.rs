//! The HTTP API: what callers send and get (wire types), and a handler for
//! each route. `/openapi.json` is generated from the handlers' types.

use aide::axum::ApiRouter;
use aide::axum::routing::{get_with, post_with};
use axum::Json;
use axum::extract::State;
use schemars::JsonSchema;
use serde::{Deserialize, Serialize};

use crate::schema::Addition;
use crate::{Bog, Problem, bog};

// Wire types are not the stored rows: these change freely, those do not.

#[derive(Deserialize, JsonSchema)]
pub struct Add {
    /// Any integer but 0.
    amount: i64,
}

#[derive(Serialize, JsonSchema)]
pub struct Sum {
    sum: i64,
}

pub fn routes() -> ApiRouter<Bog> {
    ApiRouter::new()
        .api_route(
            "/sum",
            get_with(sum, |op| op.summary("The sum of everything added")),
        )
        .api_route(
            "/add",
            post_with(add, |op| {
                op.summary("Add an amount, and get the sum with it in")
            }),
        )
}

async fn sum(State(db): State<Bog>) -> Result<Json<Sum>, Problem> {
    // one read is one snapshot: everything read in it agrees
    let sum = bog(&db, |db| db.read(|r| r.sum.get())).await?;
    Ok(Json(Sum {
        sum: sum.unwrap_or(0),
    }))
}

async fn add(State(db): State<Bog>, Json(add): Json<Add>) -> Result<Json<Sum>, Problem> {
    let sum = bog(&db, move |db| {
        // one write is one transaction: all of it commits, or none
        db.write(|tx| tx.additions.push(Addition { amount: add.amount }))?;
        // committed: reads from here on see it, and the view kept up with it
        db.read(|r| r.sum.get())
    })
    .await?;
    Ok(Json(Sum {
        sum: sum.unwrap_or(0),
    }))
}
