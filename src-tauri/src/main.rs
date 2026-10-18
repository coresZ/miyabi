#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

//! Miyabi desktop shell. First launch opens a local setup page. Later launches
//! start the Go server from `runtime/`, then show a native window loading the
//! local UI.

mod commands;
mod job;
mod log;
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
            log::init(paths.prefs_path.parent().unwrap_or(std::path::Path::new(".")));
            log::info(format!(
                "shell starting; portable={}; data={}; prefs={}",
                paths.portable,
                paths.default_data_dir.display(),
                paths.prefs_path.display()
            ));
            let ready = Prefs::load(&paths.prefs_path).is_some_and(|prefs| prefs.setup_done);
            app.manage(ShellState { paths: paths.clone() });
            if ready {
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
                        if let Err(err) = window::open_main(app.handle(), url) {
                            log::error(format!("cannot open the main window: {err}"));
                            return Err(err.into());
                        }
                        log::info("main window opened");
                    }
                    Err(err) => {
                        log::error(format!("server failed to start: {err}"));
                        return Err(std::io::Error::other(err).into());
                    }
                }
            } else {
                log::info("no setup yet; opening the setup window");
                if let Err(err) = window::open_setup(app.handle()) {
                    log::error(format!("cannot open the setup window: {err}"));
                    return Err(err.into());
                }
            }
            Ok(())
        })
        .invoke_handler(tauri::generate_handler![
            commands::desktop_defaults,
            commands::pick_data_dir,
            commands::complete_setup,
            commands::desktop_config,
            commands::apply_desktop,
            commands::set_player_window_mode,
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
