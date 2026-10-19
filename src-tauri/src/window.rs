//! Splash and setup are local pages. The main window loads the Go server over
//! loopback.

use tauri::{AppHandle, Manager, WebviewUrl, WebviewWindowBuilder};
use tauri::utils::config::Color;

const SPLASH_R: u8 = 9;
const SPLASH_G: u8 = 9;
const SPLASH_B: u8 = 11;

fn splash_bg() -> Color {
    Color(SPLASH_R, SPLASH_G, SPLASH_B, 255)
}

pub fn open_splash(app: &AppHandle) -> tauri::Result<()> {
    if app.get_webview_window("splash").is_some() {
        return Ok(());
    }
    crate::log::info("opening the splash window");
    WebviewWindowBuilder::new(app, "splash", WebviewUrl::App("splash.html".into()))
        .title("miyabi")
        .inner_size(320.0, 320.0)
        .resizable(false)
        .maximizable(false)
        .decorations(false)
        .skip_taskbar(true)
        .center()
        .background_color(splash_bg())
        .build()?;
    Ok(())
}

pub fn close_splash(app: &AppHandle) {
    if let Some(splash) = app.get_webview_window("splash") {
        let _ = splash.close();
    }
}

pub fn open_setup(app: &AppHandle) -> tauri::Result<()> {
    if app.get_webview_window("setup").is_some() {
        return Ok(());
    }
    crate::log::info("opening the setup window");
    WebviewWindowBuilder::new(app, "setup", WebviewUrl::App("setup.html".into()))
        .title("miyabi")
        .inner_size(560.0, 640.0)
        .min_inner_size(480.0, 560.0)
        .resizable(true)
        .maximizable(false)
        .center()
        .build()?;
    Ok(())
}

pub fn open_main(app: &AppHandle, url: String) -> tauri::Result<()> {
    crate::log::info(format!("opening the main window at {url}"));
    if let Some(existing) = app.get_webview_window("main") {
        let parsed: tauri::Url = url.parse().expect("local url must be valid");
        existing.navigate(parsed)?;
        let _ = existing.show();
        let _ = existing.set_focus();
        return Ok(());
    }
    let parsed: tauri::Url = url.parse().expect("local url must be valid");
    WebviewWindowBuilder::new(app, "main", WebviewUrl::External(parsed))
        .title("miyabi")
        .inner_size(1280.0, 820.0)
        // Keep the lg breakpoint reachable: below 1024 the player would fall
        // back to the mobile sheet inside a desktop window.
        .min_inner_size(1024.0, 640.0)
        // Maximizing changes the player window enough to break the page layout;
        // the shell only supports normal resizing and the player's own modes.
        .maximizable(false)
        .center()
        // Hidden until the page has loaded, so the first frame is never empty.
        .visible(false)
        .background_color(splash_bg())
        .on_page_load(|window, payload| {
            if matches!(payload.event(), tauri::webview::PageLoadEvent::Finished) {
                let _ = window.show();
                let _ = window.set_focus();
                // Close the splash only once the real UI has painted, so there
                // is no gap with no window on screen.
                close_splash(window.app_handle());
            }
        })
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
        return;
    }
    if let Some(existing) = app.get_webview_window("splash") {
        let _ = existing.show();
        let _ = existing.set_focus();
    }
}
