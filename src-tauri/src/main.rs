#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

//! Miyabi desktop shell. The splash window appears immediately, the Go server
//! boots off the main thread, then the real window replaces it.

mod commands;
mod job;
mod log;
mod prefs;
mod sidecar;
mod window;

use std::time::{Duration, Instant};

use tauri::{AppHandle, Manager};
use tauri_plugin_dialog::DialogExt;

use commands::ShellState;
use prefs::{Paths, Prefs};

/// A splash that blinks in and out reads as a glitch. Keep it up long enough
/// for the animation to register, however fast the server is.
const MIN_SPLASH: Duration = Duration::from_millis(600);

fn main() {
    tauri::Builder::default()
        .plugin(tauri_plugin_single_instance::init(|app, _argv, _cwd| {
            window::focus_existing(app);
        }))
        .plugin(tauri_plugin_dialog::init())
        .setup(|app| {
            let paths = Paths::resolve(app.handle()).map_err(std::io::Error::other)?;
            log::init(paths.prefs_path.parent().unwrap_or(std::path::Path::new(".")));
            log::info(format!(
                "shell starting; portable={}; data={}; prefs={}",
                paths.portable,
                paths.default_data_dir.display(),
                paths.prefs_path.display()
            ));
            app.manage(ShellState { paths: paths.clone() });

            // The splash must paint before the server starts; `setup` blocks the
            // event loop, so the whole boot runs on a worker thread instead.
            window::open_splash(app.handle())?;
            let handle = app.handle().clone();
            let _ = tauri::async_runtime::spawn_blocking(move || boot(handle, paths));
            Ok(())
        })
        .invoke_handler(tauri::generate_handler![
            commands::desktop_defaults,
            commands::pick_data_dir,
            commands::complete_setup,
            commands::desktop_config,
            commands::apply_desktop,
            commands::enter_player_immersive,
            commands::restore_normal_window,
        ])
        .build(tauri::generate_context!())
        .expect("error while building the Miyabi desktop shell")
        .run(|_app, event| {
            if let tauri::RunEvent::Exit = event {
                log::info("shell exiting; stopping the server");
                sidecar::shutdown();
            }
        });
}

/// Runs on a worker thread: starts the server (or shows the setup wizard) and
/// swaps the splash for the real window on the main thread.
fn boot(handle: AppHandle, paths: Paths) {
    let started = Instant::now();
    let ready = Prefs::load(&paths.prefs_path).is_some_and(|prefs| prefs.setup_done);
    if !ready {
        log::info("no setup yet; opening the setup window");
        hold_splash(started);
        let main_handle = handle.clone();
        let _ = handle.run_on_main_thread(move || {
            match window::open_setup(&main_handle) {
                Ok(()) => window::close_splash(&main_handle),
                Err(err) => fatal(main_handle, format!("无法打开配置窗口：{err}")),
            }
        });
        return;
    }

    let prefs = Prefs::load(&paths.prefs_path).expect("prefs just loaded");
    log::info(format!(
        "setup done; lan={}; port={}; password={}",
        prefs.lan,
        prefs.port,
        !prefs.password.is_empty()
    ));
    match sidecar::start(&prefs) {
        Ok(url) => {
            log::info(format!("server ready at {url}"));
            hold_splash(started);
            let main_handle = handle.clone();
            let _ = handle.run_on_main_thread(move || {
                match window::open_main(&main_handle, url) {
                    Ok(()) => log::info("main window opened"),
                    Err(err) => fatal(main_handle, format!("无法打开主窗口：{err}")),
                }
            });
            // The window is shown by its page-load handler; if that never fires,
            // show it anyway and drop the splash instead of leaving a hidden app.
            let fallback = handle.clone();
            std::thread::spawn(move || {
                std::thread::sleep(Duration::from_secs(10));
                let main_handle = fallback.clone();
                let _ = fallback.run_on_main_thread(move || {
                    if let Some(window) = main_handle.get_webview_window("main") {
                        let _ = window.show();
                    }
                    window::close_splash(&main_handle);
                });
            });
        }
        Err(err) => fatal(handle, err),
    }
}

/// Waits out the remainder of the splash's minimum on-screen time.
fn hold_splash(started: Instant) {
    let elapsed = started.elapsed();
    if elapsed < MIN_SPLASH {
        std::thread::sleep(MIN_SPLASH - elapsed);
    }
}

/// Shows a fatal startup error and quits when the user dismisses it.
///
/// The splash stays open on purpose: it is the last window, and closing it
/// would end the app before the dialog is ever seen.
fn fatal(handle: AppHandle, err: String) {
    log::error(format!("fatal startup error: {err}"));
    let main_handle = handle.clone();
    let _ = handle.run_on_main_thread(move || {
        let dialog_handle = main_handle.clone();
        main_handle
            .dialog()
            .message(err)
            .title("miyabi")
            .show(move |_| dialog_handle.exit(1));
    });
}
