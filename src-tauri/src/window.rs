//! Setup is a local page. The main window loads the Go server over loopback.

use tauri::{AppHandle, Manager, WebviewUrl, WebviewWindowBuilder};

pub fn open_setup(app: &AppHandle) -> tauri::Result<()> {
    if app.get_webview_window("setup").is_some() {
        return Ok(());
    }
    WebviewWindowBuilder::new(app, "setup", WebviewUrl::App("setup.html".into()))
        .title("Miyabi")
        .inner_size(560.0, 640.0)
        .min_inner_size(480.0, 560.0)
        .resizable(true)
        .center()
        .build()?;
    Ok(())
}

pub fn open_main(app: &AppHandle, url: String) -> tauri::Result<()> {
    if let Some(existing) = app.get_webview_window("main") {
        let parsed: tauri::Url = url.parse().expect("local url must be valid");
        existing.navigate(parsed)?;
        let _ = existing.show();
        let _ = existing.set_focus();
        return Ok(());
    }
    let parsed: tauri::Url = url.parse().expect("local url must be valid");
    WebviewWindowBuilder::new(app, "main", WebviewUrl::External(parsed))
        .title("Miyabi")
        .inner_size(1280.0, 820.0)
        .min_inner_size(960.0, 640.0)
        .center()
        .build()?;
    Ok(())
}

pub fn focus_existing(app: &AppHandle) {
    if let Some(existing) = app.get_webview_window("main") {
        let _ = existing.show();
        let _ = existing.set_focus();
        return;
    }
    if let Some(existing) = app.get_webview_window("setup") {
        let _ = existing.show();
        let _ = existing.set_focus();
    }
}
