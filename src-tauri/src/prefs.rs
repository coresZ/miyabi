//! Desktop-only preferences. Installer and portable builds share one shell.
//! The portable marker lives in `runtime/`, next to the Go server, so the
//! folder the user opens only shows miyabi.exe.

use std::path::{Path, PathBuf};

use serde::{Deserialize, Serialize};
use tauri::{AppHandle, Manager};

pub const MARKER: &str = "portable";
const FILE_NAME: &str = "desktop.json";
pub const DEFAULT_PORT: u16 = 8080;

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct Prefs {
    pub setup_done: bool,
    pub data_dir: String,
    pub lan: bool,
    pub port: u16,
    pub password: String,
}

#[derive(Clone)]
pub struct Paths {
    pub portable: bool,
    pub prefs_path: PathBuf,
    pub default_data_dir: PathBuf,
}

impl Paths {
    pub fn ui_state_path(&self) -> PathBuf {
        self.prefs_path
            .parent()
            .unwrap_or_else(|| Path::new("."))
            .join("ui-state.json")
    }

    pub fn resolve(app: &AppHandle) -> Result<Self, String> {
        let exe = std::env::current_exe().map_err(|err| err.to_string())?;
        let exe_dir = exe
            .parent()
            .ok_or_else(|| "无法定位程序目录".to_string())?
            .to_path_buf();
        let runtime = exe_dir.join("runtime");
        let portable = runtime.join(MARKER).is_file();
        if portable {
            Ok(Self {
                portable: true,
                prefs_path: runtime.join(FILE_NAME),
                default_data_dir: exe_dir.join("data"),
            })
        } else {
            let app_data = app
                .path()
                .app_data_dir()
                .map_err(|err| err.to_string())?;
            Ok(Self {
                portable: false,
                prefs_path: app_data.join(FILE_NAME),
                default_data_dir: app_data,
            })
        }
    }
}

impl Prefs {
    pub fn load(path: &Path) -> Option<Self> {
        let text = std::fs::read_to_string(path).ok()?;
        serde_json::from_str(&text).ok()
    }

    pub fn save(&self, path: &Path) -> Result<(), String> {
        if let Some(parent) = path.parent() {
            std::fs::create_dir_all(parent).map_err(|err| err.to_string())?;
        }
        let text = serde_json::to_string_pretty(self).map_err(|err| err.to_string())?;
        std::fs::write(path, text).map_err(|err| err.to_string())
    }
}

pub fn validate_port(port: u16) -> Result<(), String> {
    if port == 0 {
        return Err("端口无效".into());
    }
    Ok(())
}

/// Ensures a LAN listener has an access password. `incoming` None keeps the saved one.
pub fn effective_password(saved: &str, incoming: Option<&str>) -> String {
    match incoming {
        Some(value) => value.to_string(),
        None => saved.to_string(),
    }
}

pub fn require_lan_password(lan: bool, password: &str) -> Result<(), String> {
    if lan && password.is_empty() {
        return Err("开启局域网访问需要设置访问密码".into());
    }
    Ok(())
}
