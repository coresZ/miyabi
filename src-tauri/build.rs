fn main() {
    tauri_build::try_build(
        tauri_build::Attributes::new().app_manifest(
            tauri_build::AppManifest::new().commands(&[
                "desktop_defaults",
                "complete_setup",
                "desktop_config",
                "apply_desktop",
            ]),
        ),
    )
    .expect("failed to run tauri build script")
}
