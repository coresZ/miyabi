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

/// Sizes the normal window to 80% of the monitor's logical work area, with a
/// small floor for tiny screens. Logical units keep the CSS layout stable across
/// DPI scales; the fraction keeps the window's share of the screen stable.
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
    let width = (work_width * 0.8)
        .max(960.0_f64.min(work_width * 0.95))
        .min(work_width * 0.95);
    let height = (work_height * 0.8)
        .max(600.0_f64.min(work_height * 0.95))
        .min(work_height * 0.95);
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
