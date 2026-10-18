//! The only place that couples to the Go server's contract.

use std::net::TcpListener;
use std::sync::Mutex;
use std::time::{Duration, Instant};

use tauri::{AppHandle, Manager};
use tauri_plugin_shell::process::{CommandChild, CommandEvent};
use tauri_plugin_shell::ShellExt;

use crate::prefs::Prefs;

static CHILD: Mutex<Option<CommandChild>> = Mutex::new(None);
static PORT: Mutex<u16> = Mutex::new(0);

const READY_TIMEOUT: Duration = Duration::from_secs(30);

pub fn local_url() -> String {
    let port = *PORT.lock().expect("port lock");
    if port == 0 {
        return String::new();
    }
    format!("http://127.0.0.1:{port}")
}

/// Stops the current sidecar, starts the next one, and returns the loopback URL.
pub fn restart(app: &AppHandle, prefs: &Prefs) -> Result<String, String> {
    shutdown();
    start(app, prefs)
}

pub fn start(app: &AppHandle, prefs: &Prefs) -> Result<String, String> {
    let port = choose_port(prefs)?;
    std::fs::create_dir_all(&prefs.data_dir).map_err(|err| err.to_string())?;

    let listen = if prefs.lan {
        format!("0.0.0.0:{port}")
    } else {
        format!("127.0.0.1:{port}")
    };
    let mut command = app
        .shell()
        .sidecar("miyabi-server")
        .map_err(|err| err.to_string())?
        .env("MIYABI_LISTEN", listen)
        .env("MIYABI_DATA_DIR", &prefs.data_dir);
    if !prefs.password.is_empty() {
        command = command.env("MIYABI_ACCESS_PASSWORD", &prefs.password);
    }

    let (mut events, child) = command.spawn().map_err(|err| err.to_string())?;
    *CHILD.lock().expect("child lock") = Some(child);
    tauri::async_runtime::spawn(async move {
        while let Some(event) = events.recv().await {
            if let CommandEvent::Stdout(_) | CommandEvent::Stderr(_) = event {}
        }
    });

    if !wait_ready(port, READY_TIMEOUT) {
        shutdown();
        return Err("服务未能在 30 秒内启动".into());
    }
    *PORT.lock().expect("port lock") = port;
    Ok(format!("http://127.0.0.1:{port}"))
}

pub fn shutdown() {
    if let Some(child) = CHILD.lock().expect("child lock").take() {
        let _ = child.kill();
    }
    *PORT.lock().expect("port lock") = 0;
}

fn choose_port(prefs: &Prefs) -> Result<u16, String> {
    if prefs.lan {
        ensure_port_free(prefs.port)?;
        return Ok(prefs.port);
    }
    free_port().map_err(|err| err.to_string())
}

fn ensure_port_free(port: u16) -> Result<(), String> {
    match TcpListener::bind(("0.0.0.0", port)) {
        Ok(listener) => {
            drop(listener);
            Ok(())
        }
        Err(_) => Err("端口已被占用".into()),
    }
}

fn free_port() -> std::io::Result<u16> {
    let listener = TcpListener::bind("127.0.0.1:0")?;
    let port = listener.local_addr()?.port();
    drop(listener);
    Ok(port)
}

/// TCP connect is the readiness check. Upstream removed GET /api/health.
fn wait_ready(port: u16, timeout: Duration) -> bool {
    let addr = format!("127.0.0.1:{port}");
    let deadline = Instant::now() + timeout;
    while Instant::now() < deadline {
        if std::net::TcpStream::connect(&addr).is_ok() {
            return true;
        }
        std::thread::sleep(Duration::from_millis(200));
    }
    false
}
