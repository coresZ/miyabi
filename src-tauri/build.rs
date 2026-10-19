fn main() {
    tauri_build::try_build(
        tauri_build::Attributes::new().app_manifest(
            tauri_build::AppManifest::new().commands(&[
                "desktop_defaults",
                "pick_data_dir",
                "complete_setup",
                "desktop_config",
                "apply_desktop",
                "enter_player_immersive",
                "restore_normal_window",
            ]),
        ),
    )
    .expect("failed to run tauri build script")
}
