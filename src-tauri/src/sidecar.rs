//! The only place that couples to the Go server's contract.
//!
//! The server is a normal child at `runtime/miyabi-server.exe`, not a Tauri
//! sidecar. A sidecar is installed next to the shell, which puts two exes in
//! the folder the user opens.

use std::net::TcpListener;
use std::path::PathBuf;
use std::process::{Child, Command, Stdio};
use std::sync::Mutex;
use std::time::{Duration, Instant};

use crate::prefs::Prefs;

static CHILD: Mutex<Option<Child>> = Mutex::new(None);
static PORT: Mutex<u16> = Mutex::new(0);

const READY_TIMEOUT: Duration = Duration::from_secs(30);

pub fn local_url() -> String {
    let port = *PORT.lock().expect("port lock");
    if port == 0 {
        return String::new();
    }
    format!("http://127.0.0.1:{port}")
}

/// Stops the current server, starts the next one, and returns the loopback URL.
pub fn restart(prefs: &Prefs) -> Result<String, String> {
    shutdown();
    start(prefs)
}

pub fn start(prefs: &Prefs) -> Result<String, String> {
    let port = choose_port(prefs)?;
    std::fs::create_dir_all(&prefs.data_dir).map_err(|err| err.to_string())?;

    let listen = if prefs.lan {
        format!("0.0.0.0:{port}")
    } else {
        format!("127.0.0.1:{port}")
    };
    let server = server_path()?;
    let mut command = Command::new(&server);
    command
        .env("MIYABI_LISTEN", listen)
        .env("MIYABI_DATA_DIR", &prefs.data_dir)
        .current_dir(server.parent().unwrap_or(std::path::Path::new(".")))
        .stdin(Stdio::null())
        .stdout(Stdio::null())
        .stderr(Stdio::null());
    if !prefs.password.is_empty() {
        command.env("MIYABI_ACCESS_PASSWORD", &prefs.password);
    }

    let child = command.spawn().map_err(|err| {
        format!("找不到 runtime\\miyabi-server.exe，请不要把 Miyabi.exe 单独拷走（{err}）")
    })?;
    *CHILD.lock().expect("child lock") = Some(child);

    if !wait_ready(port, READY_TIMEOUT) {
        shutdown();
        return Err("服务未能在 30 秒内启动".into());
    }
    *PORT.lock().expect("port lock") = port;
    Ok(format!("http://127.0.0.1:{port}"))
}

pub fn shutdown() {
    if let Some(mut child) = CHILD.lock().expect("child lock").take() {
        let _ = child.kill();
        let _ = child.wait();
    }
    *PORT.lock().expect("port lock") = 0;
}

fn server_path() -> Result<PathBuf, String> {
    let exe = std::env::current_exe().map_err(|err| err.to_string())?;
    let dir = exe
        .parent()
        .ok_or_else(|| "无法定位程序目录".to_string())?;
    Ok(dir.join("runtime").join("miyabi-server.exe"))
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
