use std::{
    error::Error,
    net::TcpStream,
    path::PathBuf,
    sync::Mutex,
    thread,
    time::{Duration, Instant},
};

use serde_json::json;
use tauri::{
    menu::{Menu, MenuItem},
    tray::{MouseButton, MouseButtonState, TrayIconBuilder, TrayIconEvent},
    App, AppHandle, Manager, RunEvent, WebviewUrl, WebviewWindow, WebviewWindowBuilder,
};
use tauri_plugin_shell::{process::CommandChild, ShellExt};

const DASHBOARD_ADDR: &str = "127.0.0.1:9099";
const WINDOW_LABEL: &str = "main";

struct SidecarState(Mutex<Option<CommandChild>>);
type AppResult<T> = Result<T, Box<dyn Error>>;

pub fn run() {
    tauri::Builder::default()
        .plugin(tauri_plugin_shell::init())
        .setup(|app| {
            app.manage(SidecarState(Mutex::new(None)));
            setup_tray(app)?;
            let window = open_bootstrap_window(app)?;
            set_bootstrap_status(
                Some(&window),
                "Starting MIRAGE core",
                "Preparing the local sidecar and checking the dashboard listener.",
            );
            start_bootstrap_flow(app.handle().clone());
            Ok(())
        })
        .build(tauri::generate_context!())
        .expect("failed to build tauri app")
        .run(|app, event| {
            match event {
                RunEvent::WindowEvent { event, .. } => {
                    if let tauri::WindowEvent::CloseRequested { api, .. } = event {
                        api.prevent_close();
                        hide_window(app);
                    }
                }
                RunEvent::Exit | RunEvent::ExitRequested { .. } => {
                    shutdown_sidecar(app);
                }
                _ => {}
            }
        });
}

fn setup_tray(app: &App) -> AppResult<()> {
    let show = MenuItem::with_id(app, "show", "Open MIRAGE", true, None::<&str>)?;
    let quit = MenuItem::with_id(app, "quit", "Quit", true, None::<&str>)?;
    let menu = Menu::with_items(app, &[&show, &quit])?;

    TrayIconBuilder::new()
        .menu(&menu)
        .show_menu_on_left_click(false)
        .on_menu_event(|app, event| match event.id.as_ref() {
            "show" => show_window(app),
            "quit" => {
                shutdown_sidecar(app);
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
                show_window(&tray.app_handle());
            }
        })
        .build(app)?;
    Ok(())
}

fn start_bootstrap_flow(app: AppHandle) {
    thread::spawn(move || {
        let result = bootstrap_sidecar(&app);
        match result {
            Ok(()) => {
                mark_desktop_ready(&app);
            }
            Err(err) => {
                show_bootstrap_error(&app, &err.to_string());
            }
        }
    });
}

fn bootstrap_sidecar(app: &AppHandle) -> AppResult<()> {
    set_bootstrap_status(
        app.get_webview_window(WINDOW_LABEL).as_ref(),
        "Checking local dashboard",
        "If MIRAGE is already running, the desktop shell will attach to the existing listener.",
    );
    if wait_for_dashboard(DASHBOARD_ADDR, Duration::from_millis(600)).is_ok() {
        return Ok(());
    }

    set_bootstrap_status(
        app.get_webview_window(WINDOW_LABEL).as_ref(),
        "Launching sidecar",
        "Starting miragec in sidecar mode with the shared servers.json profile store.",
    );
    launch_sidecar(app)?;

    set_bootstrap_status(
        app.get_webview_window(WINDOW_LABEL).as_ref(),
        "Waiting for dashboard",
        "The desktop shell is waiting for the MIRAGE core to expose http://127.0.0.1:9099.",
    );
    wait_for_dashboard(DASHBOARD_ADDR, Duration::from_secs(15))?;
    Ok(())
}

fn open_bootstrap_window(app: &App) -> AppResult<WebviewWindow> {
    if let Some(window) = app.get_webview_window(WINDOW_LABEL) {
        return Ok(window);
    }

    Ok(WebviewWindowBuilder::new(
        app,
        WINDOW_LABEL,
        WebviewUrl::App("index.html".into()),
    )
        .title("MIRAGE")
        .inner_size(1280.0, 860.0)
        .min_inner_size(1080.0, 720.0)
        .center()
        .resizable(true)
        .build()
        .map(|window| window)?)
}

fn launch_sidecar(app: &AppHandle) -> AppResult<()> {
    let app_data_dir = app.path().app_data_dir()?;
    std::fs::create_dir_all(&app_data_dir)?;

    let servers_file = app_data_dir.join("servers.json");
    ensure_servers_file_seeded(&servers_file)?;

    let sidecar = app.shell().sidecar("miragec-sidecar")?;
    let (_rx, child) = sidecar
        .args(["--no-browser", "--servers", servers_file.to_string_lossy().as_ref()])
        .spawn()?;

    if let Some(state) = app.try_state::<SidecarState>() {
        if let Ok(mut guard) = state.0.lock() {
            *guard = Some(child);
        }
    }
    Ok(())
}

fn ensure_servers_file_seeded(servers_file: &PathBuf) -> AppResult<()> {
    if !servers_file.exists() {
        std::fs::write(servers_file, b"[]")?;
    }

    let current = std::fs::read_to_string(servers_file)?;
    if json_array_has_entries(&current) {
        return Ok(());
    }

    if let Some(legacy) = find_legacy_servers_json() {
        let legacy_text = std::fs::read_to_string(&legacy)?;
        if json_array_has_entries(&legacy_text) {
            std::fs::write(servers_file, legacy_text)?;
        }
    }
    Ok(())
}

fn json_array_has_entries(text: &str) -> bool {
    match serde_json::from_str::<serde_json::Value>(text) {
        Ok(serde_json::Value::Array(items)) => !items.is_empty(),
        _ => false,
    }
}

fn find_legacy_servers_json() -> Option<PathBuf> {
    let mut candidates = Vec::new();

    if let Ok(dir) = std::env::current_dir() {
        candidates.push(dir.join("..").join("servers.json"));
        candidates.push(dir.join("servers.json"));
    }
    if let Ok(exe) = std::env::current_exe() {
        if let Some(parent) = exe.parent() {
            candidates.push(parent.join("servers.json"));
            candidates.push(parent.join("..").join("..").join("..").join("servers.json"));
            candidates.push(parent.join("..").join("..").join("..").join("..").join("servers.json"));
        }
    }

    candidates
        .into_iter()
        .filter_map(|p| p.canonicalize().ok())
        .find(|p| p.exists() && p.is_file())
}

fn wait_for_dashboard(addr: &str, timeout: Duration) -> AppResult<()> {
    let started = Instant::now();
    while started.elapsed() < timeout {
        if TcpStream::connect(addr).is_ok() {
            return Ok(());
        }
        thread::sleep(Duration::from_millis(200));
    }
    Err(format!("dashboard did not start on {}", addr).into())
}

fn mark_desktop_ready(app: &AppHandle) {
    if let Some(window) = app.get_webview_window(WINDOW_LABEL) {
        set_bootstrap_status(
            Some(&window),
            "Desktop client ready",
            "The MIRAGE backend is online. Handing control to the packaged desktop interface.",
        );
        let script = "window.MIRAGE_BOOTSTRAP && window.MIRAGE_BOOTSTRAP.setReady();".to_string();
        let _ = window.eval(&script);
    }
}

fn show_window<M: Manager>(manager: &M) {
    if let Some(window) = manager.get_webview_window(WINDOW_LABEL) {
        let _ = window.show();
        let _ = window.unminimize();
        let _ = window.set_focus();
    }
}

fn hide_window<M: Manager>(manager: &M) {
    if let Some(window) = manager.get_webview_window(WINDOW_LABEL) {
        let _ = window.hide();
    }
}

fn show_bootstrap_error(app: &AppHandle, message: &str) {
    if let Some(window) = app.get_webview_window(WINDOW_LABEL) {
        let script = format!(
            "window.MIRAGE_BOOTSTRAP && window.MIRAGE_BOOTSTRAP.setError({});",
            js_string(message)
        );
        let _ = window.eval(&script);
    }
}

fn set_bootstrap_status(window: Option<&WebviewWindow>, title: &str, detail: &str) {
    if let Some(window) = window {
        let script = format!(
            "window.MIRAGE_BOOTSTRAP && window.MIRAGE_BOOTSTRAP.setStatus({}, {});",
            js_string(title),
            js_string(detail)
        );
        let _ = window.eval(&script);
    }
}

fn shutdown_sidecar(app: &AppHandle) {
    if let Some(state) = app.try_state::<SidecarState>() {
        if let Ok(mut guard) = state.0.lock() {
            if let Some(child) = guard.take() {
                let _ = child.kill();
            }
        }
    }
}

fn js_string(value: &str) -> String {
    json!(value).to_string()
}

#[allow(dead_code)]
fn _resource_path(app: &App, relative: &str) -> AppResult<PathBuf> {
    app.path()
        .resource_dir()
        .map(|dir| dir.join(relative))
        .map_err(Into::into)
}
