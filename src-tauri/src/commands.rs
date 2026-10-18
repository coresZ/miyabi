use std::net::UdpSocket;

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

#[tauri::command]
pub async fn pick_data_dir(app: AppHandle) -> Result<Option<String>, String> {
    let picked = tauri::async_runtime::spawn_blocking(move || {
        app.dialog()
            .file()
            .set_title("选择数据目录")
            .blocking_pick_folder()
    })
    .await
    .map_err(|err| err.to_string())?;
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

#[tauri::command]
pub fn complete_setup(
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
    let url = sidecar::start(&prefs)?;
    let mut done = prefs;
    done.setup_done = true;
    if let Err(err) = done.save(&state.paths.prefs_path) {
        sidecar::shutdown();
        return Err(err);
    }
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
pub fn apply_desktop(
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
    next.save(&state.paths.prefs_path)?;
    match sidecar::restart(&next) {
        Ok(url) => {
            if let Some(main) = app.get_webview_window("main") {
                let parsed = url.parse().map_err(|_| "本机地址无效".to_string())?;
                main.navigate(parsed).map_err(|err| err.to_string())?;
            }
            Ok(())
        }
        Err(err) => {
            let _ = previous.save(&state.paths.prefs_path);
            if sidecar::restart(&previous).is_err() {
                return Err(format!("{err}。无法重启，已尝试恢复之前的配置"));
            }
            Err(err)
        }
    }
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
