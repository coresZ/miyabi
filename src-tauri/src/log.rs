//! Append-only log for the desktop shell.
//!
//! The shell used to fail silently: a sidecar that never became ready or a
//! rejected command left nothing behind. Everything that can stop startup or a
//! window action is written next to `desktop.json`.

use std::fs::OpenOptions;
use std::io::Write;
use std::path::{Path, PathBuf};
use std::sync::Mutex;
use std::time::{SystemTime, UNIX_EPOCH};

const FILE_NAME: &str = "miyabi-desktop.log";
const ROTATE_AT: u64 = 2 << 20;

static LOG_PATH: Mutex<Option<PathBuf>> = Mutex::new(None);

pub fn init(dir: &Path) {
    let _ = std::fs::create_dir_all(dir);
    let path = dir.join(FILE_NAME);
    if let Ok(meta) = std::fs::metadata(&path) {
        if meta.len() > ROTATE_AT {
            let _ = std::fs::remove_file(path.with_extension("log.1"));
            let _ = std::fs::rename(&path, path.with_extension("log.1"));
        }
    }
    *LOG_PATH.lock().expect("log lock") = Some(path);
}

pub fn info(message: impl AsRef<str>) {
    write("INFO", message.as_ref());
}

pub fn error(message: impl AsRef<str>) {
    write("ERROR", message.as_ref());
}

fn write(level: &str, message: &str) {
    let Some(path) = LOG_PATH.lock().expect("log lock").clone() else {
        return;
    };
    let millis = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|elapsed| elapsed.as_millis())
        .unwrap_or(0);
    if let Ok(mut file) = OpenOptions::new().create(true).append(true).open(path) {
        let _ = writeln!(file, "{millis} {level} {message}");
    }
}
