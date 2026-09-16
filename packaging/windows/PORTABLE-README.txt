PRODUCTCREW - PORTABLE
======================

1. Run ProductCrew.exe.
2. Closing the window hides the app to the system tray so background agents
   keep running. Choose Quit from the tray menu to stop the app.

Application data is stored in your Windows user profile under:
  %USERPROFILE%\.productcrew

The portable package does not install a Windows service or create shortcuts.
ProductCrew carries its local fallback runtime inside the desktop executable
and extracts it under LocalAppData when needed. A copy of productcrew-server.exe
is included next to ProductCrew.exe so the release contains the server binary as
a visible artifact too.
Required CLI tools such as Codex, GitHub CLI, Git, Node.js, and Go can be
checked and configured from Settings > CLI tools.
