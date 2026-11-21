//! The page in front of the API: the files in web/, compiled into the binary.
//! A bog runs as its binary alone, with no files beside it, so there is no
//! directory to serve from. A new file in web/ needs a line in FILES.

use axum::Router;
use axum::http::header::CONTENT_TYPE;
use axum::routing::get;

use crate::Bog;

/// Each file: the path it answers at, its content type, and its bytes.
const FILES: &[(&str, &str, &[u8])] = &[
    (
        "/",
        "text/html; charset=utf-8",
        include_bytes!("../web/index.html"),
    ),
    (
        "/app.js",
        "text/javascript; charset=utf-8",
        include_bytes!("../web/app.js"),
    ),
    (
        "/style.css",
        "text/css; charset=utf-8",
        include_bytes!("../web/style.css"),
    ),
];

pub fn routes() -> Router<Bog> {
    FILES
        .iter()
        .fold(Router::new(), |router, &(path, content_type, body)| {
            router.route(
                path,
                get(move || async move { ([(CONTENT_TYPE, content_type)], body) }),
            )
        })
}

#[cfg(test)]
mod tests {
    /// A file in web/ that FILES leaves out answers 404.
    #[test]
    fn every_file_in_web_is_served() {
        for entry in std::fs::read_dir(concat!(env!("CARGO_MANIFEST_DIR"), "/web")).unwrap() {
            let name = entry.unwrap().file_name().into_string().unwrap();
            let path = match name.as_str() {
                "index.html" => "/".to_string(),
                name => format!("/{name}"),
            };
            assert!(
                super::FILES.iter().any(|(served, ..)| *served == path),
                "web/{name} is not in FILES (src/web.rs)"
            );
        }
    }
}
