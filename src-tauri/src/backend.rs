#[cfg(any(debug_assertions, test))]
use std::io::{Read, Write};
use std::{net::SocketAddr, net::TcpStream, time::Duration};

use axum::{
    body::{to_bytes, Body},
    extract::State,
    http::{header, HeaderValue, Method, Request, Response, StatusCode},
    middleware::{self, Next},
    response::IntoResponse,
    routing::get,
    Json, Router,
};
use reqwest::Client;
use serde::Serialize;
use serde_json::Value;
use tokio::{net::TcpListener, sync::oneshot};

pub const EMBEDDED_ADDRESS: &str = "127.0.0.1:8081";
pub const LEGACY_GO_ADDRESS: &str = "127.0.0.1:18081";

#[derive(Clone)]
struct AppState {
    client: Client,
    legacy_origin: String,
}

pub struct EmbeddedBackend {
    shutdown: Option<oneshot::Sender<()>>,
}

impl EmbeddedBackend {
    pub fn shutdown(mut self) {
        if let Some(sender) = self.shutdown.take() {
            let _ = sender.send(());
        }
    }
}

#[derive(Serialize)]
struct HealthResponse {
    status: String,
    #[serde(rename = "codexAppServer")]
    codex_app_server: String,
    #[serde(rename = "aiRuntime")]
    ai_runtime: String,
    #[serde(rename = "aiProvider")]
    ai_provider: String,
    backend: &'static str,
    sidecar: &'static str,
}

pub fn is_listening(address: &str) -> bool {
    let Ok(address) = address.parse::<SocketAddr>() else {
        return false;
    };
    TcpStream::connect_timeout(&address, Duration::from_millis(250)).is_ok()
}

#[cfg(any(debug_assertions, test))]
pub fn is_productcrew_backend_ready() -> bool {
    let Ok(address) = EMBEDDED_ADDRESS.parse::<SocketAddr>() else {
        return false;
    };
    let Ok(mut stream) = TcpStream::connect_timeout(&address, Duration::from_millis(350)) else {
        return false;
    };

    let _ = stream.set_read_timeout(Some(Duration::from_millis(750)));
    let _ = stream.set_write_timeout(Some(Duration::from_millis(750)));

    let request = format!(
        "GET /api/health HTTP/1.1\r\nHost: {EMBEDDED_ADDRESS}\r\nConnection: close\r\n\r\n"
    );
    if stream.write_all(request.as_bytes()).is_err() {
        return false;
    }

    let mut response = String::new();
    if stream.read_to_string(&mut response).is_err() {
        return false;
    }

    is_productcrew_health_response(&response)
}

#[cfg(any(debug_assertions, test))]
fn is_productcrew_health_response(response: &str) -> bool {
    response.contains("200 OK")
        && response.contains("\"status\":\"ready\"")
        && (response.contains("\"backend\":\"rust-embedded\"")
            || (response.contains("\"codexAppServer\":") && response.contains("\"aiProvider\":")))
}

pub async fn start() -> Result<EmbeddedBackend, Box<dyn std::error::Error>> {
    if is_listening(EMBEDDED_ADDRESS) {
        return Err(format!(
            "ProductCrew embedded backend address {EMBEDDED_ADDRESS} is already in use"
        )
        .into());
    }

    let listener = TcpListener::bind(EMBEDDED_ADDRESS).await?;
    let (shutdown_sender, shutdown_receiver) = oneshot::channel::<()>();
    let state = AppState {
        client: Client::new(),
        legacy_origin: format!("http://{LEGACY_GO_ADDRESS}"),
    };
    let router = Router::new()
        .route("/api/health", get(health))
        .fallback(proxy_legacy)
        .layer(middleware::from_fn(desktop_cors))
        .with_state(state);

    tauri::async_runtime::spawn(async move {
        let result = axum::serve(listener, router)
            .with_graceful_shutdown(async {
                let _ = shutdown_receiver.await;
            })
            .await;
        if let Err(error) = result {
            eprintln!("ProductCrew embedded backend stopped unexpectedly: {error}");
        }
    });

    Ok(EmbeddedBackend {
        shutdown: Some(shutdown_sender),
    })
}

async fn health(State(state): State<AppState>) -> impl IntoResponse {
    let sidecar_url = format!("{}/api/health", state.legacy_origin);
    let mut codex_app_server = "unavailable".to_string();
    let mut ai_runtime = "unavailable".to_string();
    let mut ai_provider = "none".to_string();
    let mut sidecar = "unavailable";

    if let Ok(response) = state.client.get(sidecar_url).send().await {
        if response.status().is_success() {
            sidecar = "connected";
            if let Ok(value) = response.json::<Value>().await {
                if let Some(status) = value.get("codexAppServer").and_then(Value::as_str) {
                    codex_app_server = status.to_string();
                }
                if let Some(status) = value.get("aiRuntime").and_then(Value::as_str) {
                    ai_runtime = status.to_string();
                }
                if let Some(provider) = value.get("aiProvider").and_then(Value::as_str) {
                    ai_provider = provider.to_string();
                }
            }
        }
    }

    Json(HealthResponse {
        status: "ready".into(),
        codex_app_server,
        ai_runtime,
        ai_provider,
        backend: "rust-embedded",
        sidecar,
    })
}

async fn desktop_cors(request: Request<Body>, next: Next) -> Response<Body> {
    let allowed_origin = request
        .headers()
        .get(header::ORIGIN)
        .filter(|value| is_allowed_origin(value))
        .cloned();

    if request.method() == Method::OPTIONS && allowed_origin.is_some() {
        return with_cors_headers(
            Response::new(Body::empty()),
            allowed_origin.as_ref().unwrap(),
            &request,
        );
    }

    let private_network_requested = request
        .headers()
        .get("access-control-request-private-network")
        .is_some_and(|value| value == "true");
    let mut response = next.run(request).await;
    if let Some(origin) = allowed_origin.as_ref() {
        apply_cors_headers(&mut response, origin, private_network_requested);
    }
    response
}

fn with_cors_headers(
    mut response: Response<Body>,
    origin: &HeaderValue,
    request: &Request<Body>,
) -> Response<Body> {
    *response.status_mut() = StatusCode::NO_CONTENT;
    let private_network_requested = request
        .headers()
        .get("access-control-request-private-network")
        .is_some_and(|value| value == "true");
    apply_cors_headers(&mut response, origin, private_network_requested);
    response
}

fn apply_cors_headers(
    response: &mut Response<Body>,
    origin: &HeaderValue,
    private_network_requested: bool,
) {
    let headers = response.headers_mut();
    headers.insert(header::ACCESS_CONTROL_ALLOW_ORIGIN, origin.clone());
    headers.insert(
        header::ACCESS_CONTROL_ALLOW_METHODS,
        HeaderValue::from_static("GET, POST, PUT, PATCH, DELETE, OPTIONS"),
    );
    headers.insert(
        header::ACCESS_CONTROL_ALLOW_HEADERS,
        HeaderValue::from_static("Content-Type"),
    );
    headers.insert(header::VARY, HeaderValue::from_static("Origin"));
    if private_network_requested {
        headers.insert(
            "access-control-allow-private-network",
            HeaderValue::from_static("true"),
        );
    }
}

fn is_allowed_origin(origin: &HeaderValue) -> bool {
    matches!(
        origin.to_str(),
        Ok("http://tauri.localhost" | "https://tauri.localhost" | "tauri://localhost")
    )
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn allows_only_productcrew_desktop_origins() {
        for origin in [
            "http://tauri.localhost",
            "https://tauri.localhost",
            "tauri://localhost",
        ] {
            assert!(is_allowed_origin(&HeaderValue::from_static(origin)));
        }

        assert!(!is_allowed_origin(&HeaderValue::from_static(
            "https://example.com"
        )));
    }

    #[test]
    fn applies_desktop_cors_and_private_network_headers() {
        let origin = HeaderValue::from_static("http://tauri.localhost");
        let mut response = Response::new(Body::empty());

        apply_cors_headers(&mut response, &origin, true);

        assert_eq!(
            response.headers().get(header::ACCESS_CONTROL_ALLOW_ORIGIN),
            Some(&origin)
        );
        assert_eq!(
            response
                .headers()
                .get("access-control-allow-private-network")
                .and_then(|value| value.to_str().ok()),
            Some("true")
        );
    }

    #[test]
    fn recognizes_embedded_and_development_productcrew_backends() {
        assert!(is_productcrew_health_response(
            "HTTP/1.1 200 OK\r\n\r\n{\"status\":\"ready\",\"backend\":\"rust-embedded\"}"
        ));
        assert!(is_productcrew_health_response(
            "HTTP/1.1 200 OK\r\n\r\n{\"status\":\"ready\",\"codexAppServer\":\"connected\",\"aiProvider\":\"codex\"}"
        ));
        assert!(!is_productcrew_health_response(
            "HTTP/1.1 200 OK\r\n\r\n{\"status\":\"ready\"}"
        ));
    }
}

async fn proxy_legacy(
    State(state): State<AppState>,
    request: Request<Body>,
) -> Result<Response<Body>, Response<Body>> {
    let method = request.method().clone();
    let uri = request.uri().clone();
    let path_and_query = uri
        .path_and_query()
        .map(|value| value.as_str())
        .unwrap_or("/");
    let target_url = format!("{}{}", state.legacy_origin, path_and_query);
    let headers = request.headers().clone();
    let body = to_bytes(request.into_body(), 10 * 1024 * 1024)
        .await
        .map_err(|error| {
            error_response(
                StatusCode::BAD_REQUEST,
                &format!("read request body: {error}"),
            )
        })?;

    let mut outbound = state.client.request(method, target_url);
    for (name, value) in headers.iter() {
        if name == header::HOST || name == header::CONTENT_LENGTH {
            continue;
        }
        outbound = outbound.header(name, value);
    }

    let response = outbound.body(body).send().await.map_err(|error| {
        error_response(
            StatusCode::BAD_GATEWAY,
            &format!("legacy backend unavailable: {error}"),
        )
    })?;
    let status = response.status();
    let response_headers = response.headers().clone();
    let mut builder = Response::builder().status(status);
    for (name, value) in response_headers.iter() {
        if name == header::CONTENT_LENGTH || name == header::TRANSFER_ENCODING {
            continue;
        }
        builder = builder.header(name, value);
    }
    builder
        .body(Body::from_stream(response.bytes_stream()))
        .map_err(|error| {
            error_response(
                StatusCode::BAD_GATEWAY,
                &format!("build proxy response: {error}"),
            )
        })
}

fn error_response(status: StatusCode, message: &str) -> Response<Body> {
    let payload = serde_json::json!({ "error": message }).to_string();
    Response::builder()
        .status(status)
        .header(header::CONTENT_TYPE, "application/json; charset=utf-8")
        .body(Body::from(payload))
        .expect("static error response is valid")
}
