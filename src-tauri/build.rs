fn main() {
    println!("cargo:rerun-if-env-changed=PRODUCTCREW_VERSION");
    println!("cargo:rerun-if-changed=binaries/productcrew-server-x86_64-pc-windows-msvc.exe");
    tauri_build::build()
}
