//! Splash and setup are local pages. The main window loads the Go server over
//! loopback.

use tauri::utils::config::Color;
use tauri::{AppHandle, Manager, WebviewUrl, WebviewWindowBuilder};

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
        // Fixed size: the setup form is designed for 560x640 and long paths are
        // ellipsized instead of growing the window.
        .inner_size(560.0, 640.0)
        .resizable(false)
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
    let (width, height) = initial_window_size(app);
    WebviewWindowBuilder::new(app, "main", WebviewUrl::External(parsed))
        .title("miyabi")
        .inner_size(width, height)
        // Fixed 1280x800 at 100%; immersive mode temporarily owns the window
        // and restores this exact size afterwards.
        .resizable(false)
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

const APP_WIDTH: f64 = 1280.0;
const APP_HEIGHT: f64 = 800.0;

/// Size of the normal app pages: 1280x800 at 100%, clamped to the work area.
fn app_window_size(work_width: f64, work_height: f64) -> (f64, f64) {
    let max_width = (work_width * 0.9).max(1.0);
    let max_height = (work_height * 0.9).max(1.0);
    (
        APP_WIDTH.min(max_width).round(),
        APP_HEIGHT.min(max_height).round(),
    )
}

fn work_size_of(monitor: &tauri::Monitor) -> (f64, f64) {
    let scale = monitor.scale_factor();
    (
        f64::from(monitor.work_area().size.width) / scale,
        f64::from(monitor.work_area().size.height) / scale,
    )
}

fn primary_work_size(app: &AppHandle) -> (f64, f64) {
    app.primary_monitor()
        .ok()
        .flatten()
        .as_ref()
        .map(work_size_of)
        .unwrap_or((1600.0, 1000.0))
}

/// The app uses one fixed window for every route.
fn initial_window_size(app: &AppHandle) -> (f64, f64) {
    let (work_width, work_height) = primary_work_size(app);
    app_window_size(work_width, work_height)
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
