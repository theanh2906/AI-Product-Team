#[cfg(all(windows, any(not(debug_assertions), test)))]
use std::collections::BTreeSet;

use std::{
    env, fs,
    fs::OpenOptions,
    io::Write,
    path::{Component, Path, PathBuf},
    process::{Child, Command, Stdio},
    sync::Mutex,
    thread,
    time::{Duration, Instant},
};

use serde::Deserialize;
use sha2::{Digest, Sha256};
use tauri::{
    menu::{Menu, MenuItem},
    tray::{MouseButton, MouseButtonState, TrayIconBuilder, TrayIconEvent},
    Manager, RunEvent, WindowEvent,
};

mod backend;
mod updater;

#[cfg(windows)]
const CREATE_NO_WINDOW: u32 = 0x08000000;

#[cfg(all(windows, not(debug_assertions)))]
const PRODUCTCREW_PORTS: [u16; 3] = [8000, 8081, 18081];

const EMBEDDED_GO_RUNTIME: &[u8] = include_bytes!(concat!(
    env!("CARGO_MANIFEST_DIR"),
    "/binaries/productcrew-server-x86_64-pc-windows-msvc.exe"
));

#[derive(Default)]
struct BackendRuntime {
    sidecar: Mutex<Option<Child>>,
    embedded: Mutex<Option<backend::EmbeddedBackend>>,
    owns_backend: Mutex<bool>,
}

enum BackendStartup {
    Started,
    #[cfg(debug_assertions)]
    ReusedExisting,
}

#[derive(Deserialize)]
struct ImportedProject {
    id: String,
    path: String,
}

fn wait_for_backend(address: &str, timeout: Duration) -> bool {
    let deadline = Instant::now() + timeout;
    while Instant::now() < deadline {
        if backend::is_listening(address) {
            return true;
        }
        thread::sleep(Duration::from_millis(150));
    }
    false
}

fn runtime_directory() -> Result<PathBuf, Box<dyn std::error::Error>> {
    let base = env::var_os("LOCALAPPDATA")
        .map(PathBuf::from)
        .unwrap_or_else(env::temp_dir);
    let directory = base.join("ProductCrew").join("runtime");
    fs::create_dir_all(&directory)?;
    Ok(directory)
}

fn embedded_go_runtime_path() -> Result<PathBuf, Box<dyn std::error::Error>> {
    let digest = Sha256::digest(EMBEDDED_GO_RUNTIME);
    let suffix = digest[..8]
        .iter()
        .map(|byte| format!("{byte:02x}"))
        .collect::<String>();
    let path = runtime_directory()?.join(format!("productcrew-server-{suffix}.exe"));
    if path.exists() {
        return Ok(path);
    }

    let temporary = path.with_extension("exe.tmp");
    fs::write(&temporary, EMBEDDED_GO_RUNTIME)?;
    fs::rename(&temporary, &path)?;
    Ok(path)
}

#[cfg(windows)]
fn suppress_console_window(command: &mut Command) {
    use std::os::windows::process::CommandExt;

    command.creation_flags(CREATE_NO_WINDOW);
}

#[cfg(not(windows))]
fn suppress_console_window(_command: &mut Command) {}

#[cfg(all(windows, any(not(debug_assertions), test)))]
fn listening_processes_for_ports(output: &str, ports: &[u16]) -> BTreeSet<u32> {
    output
        .lines()
        .filter_map(|line| {
            let columns = line.split_whitespace().collect::<Vec<_>>();
            if columns.len() < 5
                || !columns[0].eq_ignore_ascii_case("TCP")
                || !columns[3].eq_ignore_ascii_case("LISTENING")
            {
                return None;
            }
            let port = columns[1].rsplit(':').next()?.parse::<u16>().ok()?;
            let pid = columns[4].parse::<u32>().ok()?;
            (ports.contains(&port) && pid != 0 && pid != std::process::id()).then_some(pid)
        })
        .collect()
}

#[cfg(all(windows, not(debug_assertions)))]
fn productcrew_port_processes() -> Result<BTreeSet<u32>, Box<dyn std::error::Error>> {
    let mut command = Command::new("netstat");
    command
        .args(["-ano", "-p", "tcp"])
        .stdin(Stdio::null())
        .stderr(Stdio::null());
    suppress_console_window(&mut command);
    let output = command.output()?;
    if !output.status.success() {
        return Err("Could not inspect ProductCrew ports with netstat".into());
    }
    Ok(listening_processes_for_ports(
        &String::from_utf8_lossy(&output.stdout),
        &PRODUCTCREW_PORTS,
    ))
}

#[cfg(all(windows, not(debug_assertions)))]
fn terminate_process_tree(pid: u32) -> Result<(), Box<dyn std::error::Error>> {
    let mut command = Command::new("taskkill");
    command
        .args(["/PID", &pid.to_string(), "/F", "/T"])
        .stdin(Stdio::null())
        .stdout(Stdio::null())
        .stderr(Stdio::null());
    suppress_console_window(&mut command);
    let status = command.status()?;
    if !status.success() && productcrew_port_processes()?.contains(&pid) {
        return Err(format!("Could not stop local ProductCrew process {pid}").into());
    }
    Ok(())
}

#[cfg(all(windows, not(debug_assertions)))]
fn prepare_production_ports() -> Result<(), Box<dyn std::error::Error>> {
    let processes = productcrew_port_processes()?;
    if !processes.is_empty() {
        record_startup_message(&format!(
            "Production pre-launch is stopping local ProductCrew processes on ports 8000, 8081, and 18081 (PIDs: {})",
            processes
                .iter()
                .map(u32::to_string)
                .collect::<Vec<_>>()
                .join(", ")
        ));
    }
    for pid in processes {
        terminate_process_tree(pid)?;
    }

    let deadline = Instant::now() + Duration::from_secs(10);
    while Instant::now() < deadline {
        if productcrew_port_processes()?.is_empty() {
            return Ok(());
        }
        thread::sleep(Duration::from_millis(150));
    }
    let remaining = productcrew_port_processes()?
        .into_iter()
        .map(|pid| pid.to_string())
        .collect::<Vec<_>>()
        .join(", ");
    Err(format!(
        "ProductCrew local processes did not release ports 8000, 8081, and 18081 (PIDs: {remaining})"
    )
    .into())
}

#[cfg(any(not(windows), debug_assertions))]
fn prepare_production_ports() -> Result<(), Box<dyn std::error::Error>> {
    Ok(())
}

fn start_legacy_sidecar(app: &tauri::AppHandle) -> Result<(), Box<dyn std::error::Error>> {
    if backend::is_listening(backend::LEGACY_GO_ADDRESS) {
        return Ok(());
    }

    let executable = embedded_go_runtime_path()?;
    let mut command = Command::new(executable);
    command
        .env("APP_ADDR", backend::LEGACY_GO_ADDRESS)
        .stdin(Stdio::null())
        .stdout(Stdio::null())
        .stderr(Stdio::null());
    suppress_console_window(&mut command);

    let child = command.spawn()?;
    *app.state::<BackendRuntime>()
        .sidecar
        .lock()
        .expect("backend process lock poisoned") = Some(child);

    if !wait_for_backend(backend::LEGACY_GO_ADDRESS, Duration::from_secs(45)) {
        stop_backend(app);
        return Err(format!(
            "Go fallback service did not listen on {} within 45 seconds",
            backend::LEGACY_GO_ADDRESS
        )
        .into());
    }

    Ok(())
}

fn start_embedded_backend(app: &tauri::AppHandle) -> Result<(), Box<dyn std::error::Error>> {
    let backend_handle = tauri::async_runtime::block_on(backend::start())?;
    *app.state::<BackendRuntime>()
        .embedded
        .lock()
        .expect("embedded backend lock poisoned") = Some(backend_handle);

    if !wait_for_backend(backend::EMBEDDED_ADDRESS, Duration::from_secs(10)) {
        stop_backend(app);
        return Err(format!(
            "Rust embedded backend did not listen on {} within 10 seconds",
            backend::EMBEDDED_ADDRESS
        )
        .into());
    }

    Ok(())
}

fn start_backend(app: &tauri::AppHandle) -> Result<BackendStartup, Box<dyn std::error::Error>> {
    prepare_production_ports()?;

    #[cfg(debug_assertions)]
    if backend::is_productcrew_backend_ready() {
        return Ok(BackendStartup::ReusedExisting);
    }

    if backend::is_listening(backend::EMBEDDED_ADDRESS) {
        return Err(format!(
            "ProductCrew cannot start because {} is used by another service",
            backend::EMBEDDED_ADDRESS
        )
        .into());
    }

    *app.state::<BackendRuntime>()
        .owns_backend
        .lock()
        .expect("backend ownership lock poisoned") = true;
    if let Err(error) = start_legacy_sidecar(app) {
        stop_backend(app);
        return Err(error);
    }
    if let Err(error) = start_embedded_backend(app) {
        stop_backend(app);
        return Err(error);
    }
    Ok(BackendStartup::Started)
}

fn record_startup_message(message: &dyn std::fmt::Display) {
    let Ok(directory) = runtime_directory() else {
        return;
    };
    let Ok(mut log) = OpenOptions::new()
        .create(true)
        .append(true)
        .open(directory.join("desktop-startup.log"))
    else {
        return;
    };
    let _ = writeln!(log, "{message}");
}

fn stop_backend(app: &tauri::AppHandle) {
    let owns_backend = app
        .try_state::<BackendRuntime>()
        .and_then(|state| state.owns_backend.lock().ok().map(|owned| *owned))
        .unwrap_or(false);
    if !owns_backend {
        return;
    }

    if let Some(embedded) = app
        .try_state::<BackendRuntime>()
        .and_then(|state| state.embedded.lock().ok()?.take())
    {
        embedded.shutdown();
    }

    let Some(child) = app
        .try_state::<BackendRuntime>()
        .and_then(|state| state.sidecar.lock().ok()?.take())
    else {
        return;
    };
    stop_child(child);
}

fn stop_child(mut child: Child) {
    let _ = child.kill();
    let _ = child.wait();
}

fn show_main_window(app: &tauri::AppHandle) {
    if let Some(window) = app.get_webview_window("main") {
        let _ = window.show();
        let _ = window.unminimize();
        let _ = window.maximize();
        let _ = window.set_focus();
    }
}

#[tauri::command]
fn minimize_window(window: tauri::WebviewWindow) -> Result<(), String> {
    window.minimize().map_err(|error| error.to_string())
}

#[tauri::command]
fn hide_main_window(window: tauri::WebviewWindow) -> Result<(), String> {
    window.hide().map_err(|error| error.to_string())
}

#[tauri::command]
fn move_to_next_monitor(window: tauri::WebviewWindow) -> Result<(), String> {
    let monitors = window
        .available_monitors()
        .map_err(|error| error.to_string())?;
    if monitors.len() < 2 {
        return Ok(());
    }
    let current = window
        .current_monitor()
        .map_err(|error| error.to_string())?;
    let current_position = current.as_ref().map(|monitor| *monitor.position());
    let index = monitors
        .iter()
        .position(|monitor| Some(*monitor.position()) == current_position)
        .unwrap_or(0);
    let target = &monitors[(index + 1) % monitors.len()];
    window.unmaximize().map_err(|error| error.to_string())?;
    window
        .set_position(tauri::Position::Physical(*target.position()))
        .map_err(|error| error.to_string())?;
    window.maximize().map_err(|error| error.to_string())
}

fn resolve_project_file(project_path: &Path, relative_path: &str) -> Result<PathBuf, String> {
    let relative = Path::new(relative_path);
    if relative.as_os_str().is_empty()
        || relative.is_absolute()
        || relative.components().any(|component| {
            matches!(
                component,
                Component::ParentDir | Component::RootDir | Component::Prefix(_)
            )
        })
    {
        return Err("The file path must be relative to the imported project.".into());
    }

    let project_root = fs::canonicalize(project_path)
        .map_err(|error| format!("Could not resolve the imported project folder: {error}"))?;
    let file_path = fs::canonicalize(project_root.join(relative))
        .map_err(|error| format!("Could not find this output file: {error}"))?;
    if !file_path.starts_with(&project_root) {
        return Err("The output file is outside the imported project.".into());
    }
    if !file_path.is_file() {
        return Err("The selected output is not a file.".into());
    }
    Ok(file_path)
}

fn available_download_path(download_directory: &Path, source: &Path) -> Result<PathBuf, String> {
    let file_name = source
        .file_name()
        .ok_or_else(|| "The selected output file has no file name.".to_string())?;
    let original = download_directory.join(file_name);
    if !original.exists() {
        return Ok(original);
    }

    let stem = source
        .file_stem()
        .ok_or_else(|| "The selected output file has no valid file name.".to_string())?
        .to_string_lossy();
    let extension = source.extension().map(|value| value.to_string_lossy());
    for suffix in 1..=10_000 {
        let candidate_name = match extension.as_deref() {
            Some(extension) => format!("{stem} ({suffix}).{extension}"),
            None => format!("{stem} ({suffix})"),
        };
        let candidate = download_directory.join(candidate_name);
        if !candidate.exists() {
            return Ok(candidate);
        }
    }

    Err("Could not create a unique file name in Downloads.".into())
}

async fn imported_project_file(project_id: &str, relative_path: &str) -> Result<PathBuf, String> {
    let projects = reqwest::Client::new()
        .get(format!("http://{}/api/projects", backend::EMBEDDED_ADDRESS))
        .send()
        .await
        .map_err(|error| format!("Could not read imported projects: {error}"))?
        .error_for_status()
        .map_err(|error| format!("Could not read imported projects: {error}"))?
        .json::<Vec<ImportedProject>>()
        .await
        .map_err(|error| format!("Could not parse imported projects: {error}"))?;
    let project = projects
        .into_iter()
        .find(|project| project.id == project_id)
        .ok_or_else(|| "The selected project is no longer imported.".to_string())?;
    resolve_project_file(Path::new(&project.path), relative_path)
}

#[cfg(windows)]
fn open_with_default_app(path: &Path) -> Result<(), String> {
    let mut command = Command::new("explorer.exe");
    command
        .arg(path)
        .stdin(Stdio::null())
        .stdout(Stdio::null())
        .stderr(Stdio::null());
    suppress_console_window(&mut command);
    command
        .spawn()
        .map(|_| ())
        .map_err(|error| format!("Windows could not open this file: {error}"))
}

#[cfg(not(windows))]
fn open_with_default_app(path: &Path) -> Result<(), String> {
    let opener = if cfg!(target_os = "macos") {
        "open"
    } else {
        "xdg-open"
    };
    Command::new(opener)
        .arg(path)
        .stdin(Stdio::null())
        .stdout(Stdio::null())
        .stderr(Stdio::null())
        .spawn()
        .map(|_| ())
        .map_err(|error| format!("The operating system could not open this file: {error}"))
}

#[tauri::command]
async fn open_project_file(project_id: String, relative_path: String) -> Result<(), String> {
    let file_path = imported_project_file(&project_id, &relative_path).await?;
    open_with_default_app(&file_path)
}

#[tauri::command]
async fn download_project_file(
    app: tauri::AppHandle,
    project_id: String,
    relative_path: String,
) -> Result<String, String> {
    let source = imported_project_file(&project_id, &relative_path).await?;
    let download_directory = app
        .path()
        .download_dir()
        .map_err(|error| format!("Could not locate the Downloads folder: {error}"))?;
    fs::create_dir_all(&download_directory)
        .map_err(|error| format!("Could not create the Downloads folder: {error}"))?;
    let destination = available_download_path(&download_directory, &source)?;
    fs::copy(&source, &destination)
        .map_err(|error| format!("Could not download this output file: {error}"))?;
    Ok(destination.to_string_lossy().into_owned())
}

fn configure_tray(app: &tauri::App) -> tauri::Result<()> {
    let show = MenuItem::with_id(app, "show", "Open ProductCrew", true, None::<&str>)?;
    let quit = MenuItem::with_id(app, "quit", "Quit", true, None::<&str>)?;
    let menu = Menu::with_items(app, &[&show, &quit])?;

    TrayIconBuilder::new()
        .icon(
            app.default_window_icon()
                .expect("application icon is missing")
                .clone(),
        )
        .tooltip("ProductCrew")
        .menu(&menu)
        .show_menu_on_left_click(false)
        .on_menu_event(|app, event| match event.id.as_ref() {
            "show" => show_main_window(app),
            "quit" => {
                stop_backend(app);
                app.exit(0);
            }
            _ => {}
        })
        .on_tray_icon_event(|tray, event| {
            if let TrayIconEvent::Click {
                button: MouseButton::Left,
                button_state: MouseButtonState::Up,
                ..
            } = event
            {
                show_main_window(tray.app_handle());
            }
        })
        .build(app)?;

    Ok(())
}

pub fn run() {
    let app = tauri::Builder::default()
        .plugin(tauri_plugin_single_instance::init(
            |app, _argv, _working_directory| {
                show_main_window(app);
            },
        ))
        .manage(BackendRuntime::default())
        .invoke_handler(tauri::generate_handler![
            minimize_window,
            hide_main_window,
            move_to_next_monitor,
            open_project_file,
            download_project_file,
            updater::check_for_update,
            updater::install_update
        ])
        .setup(|app| {
            let owns_backend = match start_backend(app.handle()) {
                Ok(backend_startup) => matches!(backend_startup, BackendStartup::Started),
                Err(error) => {
                    record_startup_message(error.as_ref());
                    false
                }
            };
            *app.state::<BackendRuntime>()
                .owns_backend
                .lock()
                .expect("backend ownership lock poisoned") = owns_backend;

            if owns_backend {
                configure_tray(app)?;
            }
            if let Some(window) = app.get_webview_window("main") {
                let _ = window.maximize();
            }
            Ok(())
        })
        .on_window_event(|window, event| {
            if let WindowEvent::CloseRequested { api, .. } = event {
                let owns_backend = window
                    .app_handle()
                    .try_state::<BackendRuntime>()
                    .and_then(|state| state.owns_backend.lock().ok().map(|owned| *owned))
                    .unwrap_or(false);
                if owns_backend {
                    api.prevent_close();
                    let _ = window.hide();
                }
            }
        })
        .build(tauri::generate_context!())
        .expect("failed to build ProductCrew desktop application");

    app.run(|app, event| {
        if matches!(event, RunEvent::Exit) {
            stop_backend(app);
        }
    });
}

#[cfg(test)]
mod tests {
    use super::{available_download_path, listening_processes_for_ports, resolve_project_file};
    use std::{
        fs,
        path::PathBuf,
        time::{SystemTime, UNIX_EPOCH},
    };

    fn test_directory() -> PathBuf {
        let suffix = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .expect("system clock should be valid")
            .as_nanos();
        std::env::temp_dir().join(format!("productcrew-open-file-{suffix}"))
    }

    #[test]
    fn finds_only_listeners_on_dedicated_productcrew_ports() {
        let output = r#"
  TCP    127.0.0.1:8000         0.0.0.0:0              LISTENING       101
  TCP    127.0.0.1:8081         0.0.0.0:0              LISTENING       202
  TCP    127.0.0.1:18081        0.0.0.0:0              LISTENING       303
  TCP    127.0.0.1:5432         0.0.0.0:0              LISTENING       404
  TCP    127.0.0.1:8081         127.0.0.1:50000        ESTABLISHED     505
"#;

        let processes = listening_processes_for_ports(output, &[8000, 8081, 18081]);

        assert_eq!(
            processes.into_iter().collect::<Vec<_>>(),
            vec![101, 202, 303]
        );
    }

    #[test]
    fn resolves_existing_file_inside_project() {
        let root = test_directory();
        let artifact = root
            .join(".productcrew")
            .join("design-artifacts")
            .join("task-1")
            .join("handoff.md");
        fs::create_dir_all(artifact.parent().unwrap()).unwrap();
        fs::write(&artifact, "handoff").unwrap();

        let resolved =
            resolve_project_file(&root, ".productcrew/design-artifacts/task-1/handoff.md").unwrap();

        assert_eq!(resolved, fs::canonicalize(&artifact).unwrap());
        fs::remove_dir_all(root).unwrap();
    }

    #[test]
    fn rejects_parent_directory_traversal() {
        let error = resolve_project_file(PathBuf::from("C:\\project").as_path(), "../secret.txt")
            .unwrap_err();
        assert!(error.contains("relative to the imported project"));
    }

    #[test]
    fn creates_a_unique_download_name_without_overwriting() {
        let root = test_directory();
        let source = root.join("source").join("handoff.md");
        let downloads = root.join("Downloads");
        fs::create_dir_all(source.parent().unwrap()).unwrap();
        fs::create_dir_all(&downloads).unwrap();
        fs::write(&source, "handoff").unwrap();
        fs::write(downloads.join("handoff.md"), "existing").unwrap();

        let destination = available_download_path(&downloads, &source).unwrap();

        assert_eq!(destination, downloads.join("handoff (1).md"));
        fs::remove_dir_all(root).unwrap();
    }
}
