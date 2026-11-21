//! What this bog stores, and what bog keeps derived from it.
//!
//! Sources hold what is written. Views are computed from them and kept up to
//! date by every commit, so no handler computes them. Rules live here too, so
//! no handler can forget one.

use bog::schema::*;

/// A stored row. Its fields are its format on disk: adding, removing or
/// retyping one breaks reading what is already stored (AGENTS.md, "Changing
/// the schema").
#[derive(Clone, Debug, PartialEq, Eq, PartialOrd, Ord, Hash, Codec)]
pub struct Addition {
    pub amount: i64,
}

#[derive(Schema)]
pub struct App {
    /// Every addition made, in order: an append-only log.
    pub additions: Events<Addition>,
    /// Their sum.
    #[view]
    pub sum: ValueView<i64>,
}

impl Declare for App {
    fn declare(b: &mut AppBuilder) {
        // a rule: a write it refuses rolls back, whichever handler made it
        b.additions.reject(|a| a.amount == 0);
        // a view: bog adds each new amount to the sum as it commits
        b.sum.define(&b.additions.map(|a| a.amount).sum());
    }
}

/// Reads one row of every source. `GET /healthz` answers with it, so a build
/// whose rows no longer read as what is stored fails its deploy instead of
/// serving errors. Add each new source here.
pub fn check(r: &AppRead<'_>) -> bog::Result<()> {
    r.additions.iter().next().transpose()?;
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    // the schema, tested without HTTP: in memory, through the same calls
    #[test]
    fn the_sum_follows_every_addition() {
        let db = App::memory().build().unwrap();
        db.write(|tx| tx.additions.push(Addition { amount: 2 }))
            .unwrap();
        db.write(|tx| tx.additions.push(Addition { amount: 40 }))
            .unwrap();
        assert_eq!(db.read(|r| r.sum.get()).unwrap(), Some(42));

        assert!(
            db.write(|tx| tx.additions.push(Addition { amount: 0 }))
                .is_err()
        );
        assert_eq!(db.read(|r| r.sum.get()).unwrap(), Some(42));
        db.read(check).unwrap();
    }
}
