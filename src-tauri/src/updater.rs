use serde::Serialize;

#[derive(Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct UpdateStatus {
    pub enabled: bool,
    pub state: String,
    pub current_version: String,
    pub latest_version: Option<String>,
    pub notes: Option<String>,
    pub message: String,
}

fn current_version() -> String {
    option_env!("PRODUCTCREW_VERSION")
        .unwrap_or(env!("CARGO_PKG_VERSION"))
        .to_string()
}

#[cfg(feature = "installer-updates")]
mod installer {
    use super::{current_version, UpdateStatus};
    use semver::Version;
    use serde::Deserialize;
    use sha2::{Digest, Sha256};
    use std::{
        fs,
        io::Read,
        path::{Path, PathBuf},
        process::Command,
    };
    use tauri::AppHandle;

    const MANIFEST_NAME: &str = "installer-latest.json";

    #[derive(Deserialize)]
    #[serde(rename_all = "camelCase")]
    struct InstallerManifest {
        version: String,
        installer: String,
        sha256: String,
        size: u64,
        notes: Option<String>,
    }

    struct VerifiedUpdate {
        manifest: InstallerManifest,
        path: PathBuf,
        available: bool,
    }

    fn verify(update_path: &str) -> Result<VerifiedUpdate, String> {
        let root = fs::canonicalize(update_path)
            .map_err(|error| format!("Update folder is unavailable: {error}"))?;
        let raw = fs::read(root.join(MANIFEST_NAME))
            .map_err(|error| format!("Could not read {MANIFEST_NAME}: {error}"))?;
        let manifest: InstallerManifest = serde_json::from_slice(&raw)
            .map_err(|error| format!("Invalid {MANIFEST_NAME}: {error}"))?;
        let relative = Path::new(&manifest.installer);
        if relative.components().count() != 1
            || relative.extension().and_then(|value| value.to_str()) != Some("exe")
        {
            return Err(
                "Update manifest must reference one installer EXE in the configured update folder."
                    .into(),
            );
        }
        let path = fs::canonicalize(root.join(relative))
            .map_err(|error| format!("Installer file is unavailable: {error}"))?;
        if !path.starts_with(&root) {
            return Err("Installer path escapes the configured update folder.".into());
        }
        let metadata =
            fs::metadata(&path).map_err(|error| format!("Could not inspect installer: {error}"))?;
        if metadata.len() != manifest.size {
            return Err(format!(
                "Installer size mismatch: expected {}, received {}.",
                manifest.size,
                metadata.len()
            ));
        }
        let mut file =
            fs::File::open(&path).map_err(|error| format!("Could not open installer: {error}"))?;
        let mut digest = Sha256::new();
        let mut buffer = [0_u8; 64 * 1024];
        loop {
            let read = file
                .read(&mut buffer)
                .map_err(|error| format!("Could not verify installer: {error}"))?;
            if read == 0 {
                break;
            }
            digest.update(&buffer[..read]);
        }
        let actual_hash = format!("{:X}", digest.finalize());
        if !actual_hash.eq_ignore_ascii_case(manifest.sha256.trim()) {
            return Err("Installer SHA-256 does not match the update manifest.".into());
        }
        let current = Version::parse(&current_version())
            .map_err(|error| format!("Invalid current app version: {error}"))?;
        let latest = Version::parse(&manifest.version)
            .map_err(|error| format!("Invalid update version: {error}"))?;
        Ok(VerifiedUpdate {
            manifest,
            path,
            available: latest > current,
        })
    }

    pub fn check(update_path: &str) -> UpdateStatus {
        match verify(update_path) {
            Ok(update) if update.available => UpdateStatus {
                enabled: true,
                state: "available".into(),
                current_version: current_version(),
                latest_version: Some(update.manifest.version),
                notes: update.manifest.notes,
                message: "A verified ProductCrew installer update is ready.".into(),
            },
            Ok(update) => UpdateStatus {
                enabled: true,
                state: "current".into(),
                current_version: current_version(),
                latest_version: Some(update.manifest.version),
                notes: update.manifest.notes,
                message: "ProductCrew is up to date.".into(),
            },
            Err(message) => UpdateStatus {
                enabled: true,
                state: "unavailable".into(),
                current_version: current_version(),
                latest_version: None,
                notes: None,
                message,
            },
        }
    }

    pub fn install(app: &AppHandle, update_path: &str) -> Result<(), String> {
        let update = verify(update_path)?;
        if !update.available {
            return Err("No newer installer update is available.".into());
        }
        Command::new(&update.path)
            .args(["/P", "/R"])
            .spawn()
            .map_err(|error| format!("Could not launch ProductCrew installer: {error}"))?;
        app.exit(0);
        Ok(())
    }

    #[cfg(test)]
    mod tests {
        use super::{verify, MANIFEST_NAME};
        use sha2::{Digest, Sha256};
        use std::{
            fs,
            path::PathBuf,
            time::{SystemTime, UNIX_EPOCH},
        };

        fn fixture(installer_name: &str, bytes: &[u8], hash: Option<String>) -> PathBuf {
            let unique = SystemTime::now()
                .duration_since(UNIX_EPOCH)
                .expect("clock should be after epoch")
                .as_nanos();
            let root = std::env::temp_dir().join(format!("productcrew-updater-{unique}"));
            fs::create_dir_all(&root).expect("fixture folder should be created");
            fs::write(root.join("ProductCrew-setup.exe"), bytes)
                .expect("fixture installer should be written");
            let sha256 = hash.unwrap_or_else(|| format!("{:X}", Sha256::digest(bytes)));
            let manifest = serde_json::json!({
                "version": "99.0.0",
                "installer": installer_name,
                "sha256": sha256,
                "size": bytes.len(),
                "notes": "Test update"
            });
            fs::write(
                root.join(MANIFEST_NAME),
                serde_json::to_vec(&manifest).expect("manifest should serialize"),
            )
            .expect("manifest should be written");
            root
        }

        #[test]
        fn accepts_a_verified_newer_installer() {
            let root = fixture("ProductCrew-setup.exe", b"verified installer", None);

            let update = verify(root.to_str().expect("fixture path should be Unicode"))
                .expect("valid update should verify");

            assert!(update.available);
            assert_eq!(update.manifest.version, "99.0.0");
            fs::remove_dir_all(root).expect("fixture should be removed");
        }

        #[test]
        fn rejects_an_installer_with_a_mismatched_hash() {
            let root = fixture("ProductCrew-setup.exe", b"tampered", Some("00".repeat(32)));

            let error = match verify(root.to_str().expect("fixture path should be Unicode")) {
                Ok(_) => panic!("tampered installer should fail verification"),
                Err(error) => error,
            };

            assert!(error.contains("SHA-256"));
            fs::remove_dir_all(root).expect("fixture should be removed");
        }

        #[test]
        fn rejects_a_manifest_path_outside_the_update_folder() {
            let root = fixture("..\\ProductCrew-setup.exe", b"installer", None);

            let error = match verify(root.to_str().expect("fixture path should be Unicode")) {
                Ok(_) => panic!("path traversal should fail verification"),
                Err(error) => error,
            };

            assert!(error.contains("one installer EXE"));
            fs::remove_dir_all(root).expect("fixture should be removed");
        }
    }
}

#[tauri::command]
pub fn check_for_update(update_path: String) -> UpdateStatus {
    #[cfg(feature = "installer-updates")]
    {
        return installer::check(&update_path);
    }
    #[cfg(not(feature = "installer-updates"))]
    {
        let _ = update_path;
        UpdateStatus {
            enabled: false,
            state: "disabled".into(),
            current_version: current_version(),
            latest_version: None,
            notes: None,
            message: "Automatic updates are available in the installed ProductCrew edition only."
                .into(),
        }
    }
}

#[tauri::command]
pub fn install_update(app: tauri::AppHandle, update_path: String) -> Result<(), String> {
    #[cfg(feature = "installer-updates")]
    {
        return installer::install(&app, &update_path);
    }
    #[cfg(not(feature = "installer-updates"))]
    {
        let _ = (app, update_path);
        Err("Automatic updates are disabled in portable builds.".into())
    }
}
