use std::net::UdpSocket;
use std::sync::Mutex;

use serde::Serialize;
use tauri::{AppHandle, Manager, State};
use tauri_plugin_dialog::DialogExt;

use crate::prefs::{self, effective_password, require_lan_password, validate_port, Paths, Prefs};
use crate::sidecar;
use crate::window;

#[derive(Clone)]
pub struct ShellState {
    pub paths: Paths,
}

#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
pub struct Defaults {
    pub portable: bool,
    pub data_dir: String,
}

#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
pub struct DesktopConfig {
    pub portable: bool,
    pub data_dir: String,
    pub lan: bool,
    pub port: u16,
    pub has_password: bool,
    pub addresses: Vec<String>,
    pub local_url: String,
}

#[tauri::command]
pub fn desktop_defaults(state: State<'_, ShellState>) -> Defaults {
    Defaults {
        portable: state.paths.portable,
        data_dir: state.paths.default_data_dir.to_string_lossy().to_string(),
    }
}

/// The folder picker must run on the main thread: Windows dialogs need the
/// event loop's COM message pump, so this stays non-blocking.
#[tauri::command]
pub async fn pick_data_dir(app: AppHandle) -> Result<Option<String>, String> {
    let (tx, rx) = std::sync::mpsc::channel();
    app.dialog()
        .file()
        .set_title("选择数据目录")
        .pick_folder(move |picked| {
            let _ = tx.send(picked);
        });
    let picked = rx.recv().map_err(|err| err.to_string())?;
    match picked {
        Some(path) => Ok(Some(
            path.into_path()
                .map_err(|_| "无法使用所选目录".to_string())?
                .to_string_lossy()
                .to_string(),
        )),
        None => Ok(None),
    }
}

/// Starting the Go server blocks for up to 30s, so it runs off the main thread;
/// blocking it froze the webview and left the window blank.
#[tauri::command]
pub async fn complete_setup(
    app: AppHandle,
    state: State<'_, ShellState>,
    data_dir: String,
    password: String,
    lan: bool,
) -> Result<(), String> {
    let data_dir = if state.paths.portable {
        state.paths.default_data_dir.to_string_lossy().to_string()
    } else {
        data_dir.trim().to_string()
    };
    if data_dir.is_empty() {
        return Err("请选择数据目录".into());
    }
    let password = password.trim().to_string();
    require_lan_password(lan, &password)?;
    let prefs = Prefs {
        setup_done: false,
        data_dir,
        lan,
        port: prefs::DEFAULT_PORT,
        password,
    };
    crate::log::info(format!(
        "setup submitted; data={}; lan={}; password={}",
        prefs.data_dir,
        prefs.lan,
        !prefs.password.is_empty()
    ));
    let to_start = prefs.clone();
    let url = match blocking(move || sidecar::start(&to_start)).await {
        Ok(url) => url,
        Err(err) => {
            crate::log::error(format!("setup could not start the server: {err}"));
            return Err(err);
        }
    };
    let mut done = prefs;
    done.setup_done = true;
    let prefs_path = state.paths.prefs_path.clone();
    if let Err(err) = blocking(move || done.save(&prefs_path)).await {
        crate::log::error(format!("setup could not save the preferences: {err}"));
        sidecar::shutdown();
        return Err(err);
    }
    crate::log::info(format!("setup finished; server at {url}"));
    window::open_main(&app, url).map_err(|err| err.to_string())?;
    if let Some(setup) = app.get_webview_window("setup") {
        let _ = setup.close();
    }
    Ok(())
}

#[tauri::command]
pub fn desktop_config(state: State<'_, ShellState>) -> DesktopConfig {
    let prefs = Prefs::load(&state.paths.prefs_path);
    let lan = prefs.as_ref().is_some_and(|prefs| prefs.lan);
    let port = prefs
        .as_ref()
        .map(|prefs| prefs.port)
        .unwrap_or(prefs::DEFAULT_PORT);
    DesktopConfig {
        portable: state.paths.portable,
        data_dir: prefs
            .as_ref()
            .map(|prefs| prefs.data_dir.clone())
            .unwrap_or_else(|| state.paths.default_data_dir.to_string_lossy().to_string()),
        lan,
        port,
        has_password: prefs.as_ref().is_some_and(|prefs| !prefs.password.is_empty()),
        addresses: if lan { lan_urls(port) } else { Vec::new() },
        local_url: sidecar::local_url(),
    }
}

#[tauri::command]
pub async fn apply_desktop(
    app: AppHandle,
    state: State<'_, ShellState>,
    lan: bool,
    port: u16,
    password: Option<String>,
) -> Result<(), String> {
    validate_port(port)?;
    let previous =
        Prefs::load(&state.paths.prefs_path).ok_or_else(|| "尚未完成首次配置".to_string())?;
    let next_password = effective_password(&previous.password, password.as_deref());
    require_lan_password(lan, &next_password)?;
    let next = Prefs {
        setup_done: true,
        data_dir: previous.data_dir.clone(),
        lan,
        port,
        password: next_password,
    };
    crate::log::info(format!(
        "applying desktop settings; lan={lan}; port={port}; password={}",
        !next.password.is_empty()
    ));
    let prefs_path = state.paths.prefs_path.clone();
    let to_save = next.clone();
    if let Err(err) = blocking(move || to_save.save(&prefs_path)).await {
        crate::log::error(format!("cannot save the desktop settings: {err}"));
        return Err(err);
    }

    let prefs_path = state.paths.prefs_path.clone();
    let restore = previous.clone();
    let to_start = next;
    match blocking(move || sidecar::restart(&to_start)).await {
        Ok(url) => {
            crate::log::info(format!("desktop settings applied; server at {url}"));
            if let Some(main) = app.get_webview_window("main") {
                let parsed = url.parse().map_err(|_| "本机地址无效".to_string())?;
                main.navigate(parsed).map_err(|err| err.to_string())?;
            }
            Ok(())
        }
        Err(err) => {
            crate::log::error(format!("restart failed: {err}; restoring the previous settings"));
            let _ = restore.save(&prefs_path);
            if blocking(move || sidecar::restart(&restore)).await.is_err() {
                crate::log::error("could not restore the previous server");
                return Err(format!("{err}。无法重启，已尝试恢复之前的配置"));
            }
            Err(err)
        }
    }
}

#[tauri::command]
pub fn set_player_window_mode(app: AppHandle, mode: String) -> Result<(), String> {
    crate::log::info(format!("player window mode -> {mode}"));
    let window = app
        .get_webview_window("main")
        .ok_or_else(|| "主窗口未打开".to_string())?;
    match mode.as_str() {
        "windowed" => {
            let _ = window.set_fullscreen(false);
            window.set_decorations(true).map_err(|err| err.to_string())?;
            if let Some(bounds) = WINDOW_BOUNDS.lock().expect("bounds lock").take() {
                let _ = window.set_size(bounds.size);
                let _ = window.set_position(bounds.position);
            }
        }
        "borderless" => {
            if WINDOW_BOUNDS.lock().expect("bounds lock").is_none() {
                let size = window.inner_size().map_err(|err| err.to_string())?;
                let position = window.outer_position().map_err(|err| err.to_string())?;
                *WINDOW_BOUNDS.lock().expect("bounds lock") = Some(WindowBounds { size, position });
            }
            let _ = window.set_fullscreen(false);
            window.set_decorations(false).map_err(|err| err.to_string())?;
        }
        "fullscreen" => {
            window.set_fullscreen(true).map_err(|err| err.to_string())?;
        }
        _ => return Err("未知的窗口模式".into()),
    }
    Ok(())
}

/// Runs a blocking task on the thread pool so the webview keeps responding.
async fn blocking<T, F>(task: F) -> Result<T, String>
where
    T: Send + 'static,
    F: FnOnce() -> Result<T, String> + Send + 'static,
{
    tauri::async_runtime::spawn_blocking(task)
        .await
        .map_err(|err| err.to_string())?
}

static WINDOW_BOUNDS: Mutex<Option<WindowBounds>> = Mutex::new(None);

struct WindowBounds {
    size: tauri::PhysicalSize<u32>,
    position: tauri::PhysicalPosition<i32>,
}

/// Best-effort LAN addresses. A UDP connect does not send a packet; it only
/// selects the outbound interface so we can show one reachable IPv4.
fn lan_urls(port: u16) -> Vec<String> {
    let Ok(socket) = UdpSocket::bind("0.0.0.0:0") else {
        return Vec::new();
    };
    if socket.connect("8.8.8.8:80").is_err() {
        return Vec::new();
    }
    match socket.local_addr() {
        Ok(addr) if !addr.ip().is_loopback() => vec![format!("http://{}:{port}", addr.ip())],
        _ => Vec::new(),
    }
}
