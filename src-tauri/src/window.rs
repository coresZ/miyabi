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
    let (width, height) = initial_window_size(app);
    WebviewWindowBuilder::new(app, "main", WebviewUrl::External(parsed))
        .title("miyabi")
        .inner_size(width, height)
        // The normal window is fixed: only the immersive player may change size,
        // and only through the shell's aspect-fitted command.
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

/// Fixed catalog width, shared by the normal play window sizing and the
/// immersive player fit (the normal player area excludes this column).
pub const CATALOG_WIDTH: f64 = 320.0;

/// Sizes the normal window so the play page fills it exactly: the player is
/// 16:9 at 80% of the monitor's logical work height, plus the fixed catalog.
fn initial_window_size(app: &AppHandle) -> (f64, f64) {
    let (work_width, work_height) = app
        .primary_monitor()
        .ok()
        .flatten()
        .map(|monitor| {
            let scale = monitor.scale_factor();
            (
                f64::from(monitor.work_area().size.width) / scale,
                f64::from(monitor.work_area().size.height) / scale,
            )
        })
        .unwrap_or((1600.0, 1000.0));
    let max_width = work_width * 0.95;
    let max_height = work_height * 0.95;

    let mut height = work_height * 0.8;
    let mut player_width = height * 16.0 / 9.0;
    let mut width = player_width + CATALOG_WIDTH;
    if width > max_width {
        width = max_width;
        player_width = (width - CATALOG_WIDTH).max(1.0);
        height = player_width * 9.0 / 16.0;
    }
    if height > max_height {
        height = max_height;
        player_width = height * 16.0 / 9.0;
        width = player_width + CATALOG_WIDTH;
        if width > max_width {
            width = max_width;
            player_width = (width - CATALOG_WIDTH).max(1.0);
            height = player_width * 9.0 / 16.0;
        }
    }
    (width.round(), height.round())
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
