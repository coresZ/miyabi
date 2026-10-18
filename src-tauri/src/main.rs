#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

//! Miyabi desktop shell. First launch opens a local setup page. Later launches
//! start the Go server from `runtime/`, then show a native window loading the
//! local UI.

mod commands;
mod prefs;
mod sidecar;
mod window;

use tauri::Manager;

use commands::ShellState;
use prefs::{Paths, Prefs};

fn main() {
    tauri::Builder::default()
        .plugin(tauri_plugin_single_instance::init(|app, _argv, _cwd| {
            window::focus_existing(app);
        }))
        .plugin(tauri_plugin_dialog::init())
        .setup(|app| {
            let paths = Paths::resolve(app.handle()).map_err(std::io::Error::other)?;
            let ready = Prefs::load(&paths.prefs_path).is_some_and(|prefs| prefs.setup_done);
            app.manage(ShellState { paths: paths.clone() });
            if ready {
                let prefs = Prefs::load(&paths.prefs_path).expect("prefs just loaded");
                let url = sidecar::start(&prefs).map_err(std::io::Error::other)?;
                window::open_main(app.handle(), url)?;
            } else {
                window::open_setup(app.handle())?;
            }
            Ok(())
        })
        .invoke_handler(tauri::generate_handler![
            commands::desktop_defaults,
            commands::pick_data_dir,
            commands::complete_setup,
            commands::desktop_config,
            commands::apply_desktop,
        ])
        .build(tauri::generate_context!())
        .expect("error while building the Miyabi desktop shell")
        .run(|_app, event| {
            if let tauri::RunEvent::Exit = event {
                sidecar::shutdown();
            }
        });
}
