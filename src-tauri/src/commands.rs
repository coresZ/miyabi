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
pub fn load_ui_state(state: State<'_, ShellState>) -> Result<serde_json::Value, String> {
    let path = state.paths.ui_state_path();
    let text = match std::fs::read_to_string(&path) {
        Ok(text) => text,
        Err(err) if err.kind() == std::io::ErrorKind::NotFound => {
            return Ok(serde_json::json!({}));
        }
        Err(err) => return Err(err.to_string()),
    };
    serde_json::from_str(&text).map_err(|err| err.to_string())
}

#[tauri::command]
pub fn save_ui_state(
    state: State<'_, ShellState>,
    value: serde_json::Value,
) -> Result<(), String> {
    let path = state.paths.ui_state_path();
    if let Some(parent) = path.parent() {
        std::fs::create_dir_all(parent).map_err(|err| err.to_string())?;
    }
    let text = serde_json::to_string_pretty(&value).map_err(|err| err.to_string())?;
    let tmp = path.with_extension("json.tmp");
    std::fs::write(&tmp, text).map_err(|err| err.to_string())?;
    let _ = std::fs::remove_file(&path);
    std::fs::rename(&tmp, &path).map_err(|err| err.to_string())
}

#[derive(Clone)]
struct WindowSnapshot {
    inner_size: tauri::PhysicalSize<u32>,
    outer_position: tauri::PhysicalPosition<i32>,
    decorated: bool,
    resizable: bool,
}

static IMMERSIVE_SNAPSHOT: Mutex<Option<WindowSnapshot>> = Mutex::new(None);
static IMMERSIVE_ASPECT: Mutex<Option<f64>> = Mutex::new(None);

/// Removes the title bar and locks the window to the video aspect ratio.
/// The normal window is never maximizable; `restore_normal_window` restores the
/// exact decorations/resizable/size/position captured here.
#[tauri::command]
pub fn enter_player_immersive(app: AppHandle, aspect: Option<f64>) -> Result<(), String> {
    let window = app
        .get_webview_window("main")
        .ok_or_else(|| "主窗口未打开".to_string())?;
    let scale = window.scale_factor().map_err(|err| err.to_string())?;
    let base_width = {
        let mut snapshot = IMMERSIVE_SNAPSHOT.lock().expect("snapshot lock");
        if snapshot.is_none() {
            *snapshot = Some(WindowSnapshot {
                inner_size: window.inner_size().map_err(|err| err.to_string())?,
                outer_position: window.outer_position().map_err(|err| err.to_string())?,
                decorated: window.is_decorated().map_err(|err| err.to_string())?,
                resizable: window.is_resizable().map_err(|err| err.to_string())?,
            });
        }
        snapshot.as_ref().map(|snapshot| {
            (f64::from(snapshot.inner_size.width) - window::CATALOG_WIDTH * scale).max(1.0)
        })
    };
    window
        .set_decorations(false)
        .map_err(|err| err.to_string())?;
    window
        .set_resizable(false)
        .map_err(|err| err.to_string())?;
    if let Some(aspect) = aspect {
        *IMMERSIVE_ASPECT.lock().expect("aspect lock") = Some(aspect);
        fit_to_aspect(&window, aspect, base_width)?;
    }
    crate::log::info("player window -> immersive");
    Ok(())
}

/// Resizes the player-only immersive window while keeping the current video
/// aspect. The page sends a physical width; the shell derives height and keeps
/// the opposite corner fixed.
#[tauri::command]
pub fn resize_player_immersive(
    app: AppHandle,
    width: f64,
    anchor: String,
) -> Result<(), String> {
    let window = app
        .get_webview_window("main")
        .ok_or_else(|| "主窗口未打开".to_string())?;
    let aspect = *IMMERSIVE_ASPECT.lock().expect("aspect lock");
    let Some(aspect) = aspect else {
        return Ok(());
    };
    if !width.is_finite() || width <= 0.0 || !(0.1..=10.0).contains(&aspect) {
        return Ok(());
    }
    let Some(monitor) = window.current_monitor().map_err(|err| err.to_string())? else {
        return Ok(());
    };
    let work = monitor.work_area();
    let scale = monitor.scale_factor();
    let max_width = f64::from(work.size.width);
    let max_height = f64::from(work.size.height);
    let min_width = 320.0 * scale;
    let min_height = 180.0 * scale;

    let mut target_width = width.max(min_width);
    let mut target_height = target_width / aspect;
    let fit = (max_width / target_width)
        .min(max_height / target_height)
        .min(1.0);
    target_width *= fit;
    target_height *= fit;
    if target_width < min_width || target_height < min_height {
        let grow = (min_width / target_width).max(min_height / target_height);
        let grown_width = target_width * grow;
        let grown_height = target_height * grow;
        if grown_width <= max_width && grown_height <= max_height {
            target_width = grown_width;
            target_height = grown_height;
        }
    }

    let current = window.inner_size().map_err(|err| err.to_string())?;
    let position = window.outer_position().map_err(|err| err.to_string())?;
    let mut x = position.x;
    let mut y = position.y;
    if anchor == "NorthWest" {
        x = position.x + current.width as i32 - target_width.round() as i32;
        y = position.y + current.height as i32 - target_height.round() as i32;
    }
    let min_x = work.position.x;
    let min_y = work.position.y;
    let max_x = (min_x + work.size.width as i32 - target_width.round() as i32).max(min_x);
    let max_y = (min_y + work.size.height as i32 - target_height.round() as i32).max(min_y);
    let x = x.clamp(min_x, max_x);
    let y = y.clamp(min_y, max_y);

    window
        .set_size(tauri::PhysicalSize::<u32>::new(
            target_width.round().max(1.0) as u32,
            target_height.round().max(1.0) as u32,
        ))
        .map_err(|err| err.to_string())?;
    window
        .set_position(tauri::PhysicalPosition::new(x, y))
        .map_err(|err| err.to_string())
}

/// Restores the state captured before entering immersive mode. Idempotent: when
/// there is no snapshot (fullscreen entered from a normal window) it only makes
/// sure the window is not fullscreen.
#[tauri::command]
pub fn restore_normal_window(app: AppHandle) -> Result<(), String> {
    let window = app
        .get_webview_window("main")
        .ok_or_else(|| "主窗口未打开".to_string())?;
    let _ = window.set_fullscreen(false);
    *IMMERSIVE_ASPECT.lock().expect("aspect lock") = None;
    // Keep the snapshot until every restore succeeds, so a failure can retry.
    let snapshot = IMMERSIVE_SNAPSHOT
        .lock()
        .expect("snapshot lock")
        .clone();
    if let Some(snapshot) = snapshot {
        window
            .set_resizable(snapshot.resizable)
            .map_err(|err| err.to_string())?;
        window
            .set_decorations(snapshot.decorated)
            .map_err(|err| err.to_string())?;
        window
            .set_size(snapshot.inner_size)
            .map_err(|err| err.to_string())?;
        window
            .set_position(snapshot.outer_position)
            .map_err(|err| err.to_string())?;
        *IMMERSIVE_SNAPSHOT.lock().expect("snapshot lock") = None;
    }
    crate::log::info("player window -> normal");
    Ok(())
}

/// Strictly proportional resize: both dimensions are multiplied by the same
/// factor. It never fixes one side independently and never stretches.
fn fit_to_aspect(
    window: &tauri::WebviewWindow,
    aspect: f64,
    base_width: Option<f64>,
) -> Result<(), String> {
    if !aspect.is_finite() || !(0.1..=10.0).contains(&aspect) {
        return Ok(());
    }
    let size = window.inner_size().map_err(|err| err.to_string())?;
    let mut width = base_width
        .unwrap_or_else(|| f64::from(size.width))
        .max(1.0);
    let mut height = width / aspect;
    let monitor = window.current_monitor().map_err(|err| err.to_string())?;
    if let Some(monitor) = monitor {
        let work = monitor.work_area();
        let work_width = f64::from(work.size.width);
        let work_height = f64::from(work.size.height);
        let scale = (work_width / width)
            .min(work_height / height)
            .min(1.0);
        width *= scale;
        height *= scale;

        let old_outer = window
            .outer_position()
            .unwrap_or_else(|_| work.position.clone());
        let old_size = window
            .outer_size()
            .unwrap_or(tauri::PhysicalSize::<u32>::new(
                width.round() as u32,
                height.round() as u32,
            ));
        let center_x = f64::from(old_outer.x) + f64::from(old_size.width) / 2.0;
        let center_y = f64::from(old_outer.y) + f64::from(old_size.height) / 2.0;
        let min_x = f64::from(work.position.x);
        let min_y = f64::from(work.position.y);
        let max_x = (min_x + work_width - width).max(min_x);
        let max_y = (min_y + work_height - height).max(min_y);
        let x = (center_x - width / 2.0).clamp(min_x, max_x);
        let y = (center_y - height / 2.0).clamp(min_y, max_y);
        window
            .set_size(tauri::PhysicalSize::<u32>::new(
                width.round().max(1.0) as u32,
                height.round().max(1.0) as u32,
            ))
            .map_err(|err| err.to_string())?;
        window
            .set_position(tauri::PhysicalPosition::new(x.round() as i32, y.round() as i32))
            .map_err(|err| err.to_string())?;
        return Ok(());
    }
    window
        .set_size(tauri::PhysicalSize::<u32>::new(
            width.round().max(1.0) as u32,
            height.round().max(1.0) as u32,
        ))
        .map_err(|err| err.to_string())
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
