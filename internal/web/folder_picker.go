package web

import (
	"errors"
	"os/exec"
	"runtime"
	"strings"
)

var errFolderPickerCancelled = errors.New("folder picker cancelled")

const windowsFolderPickerScript = `
Add-Type -AssemblyName System.Windows.Forms

$dialog = New-Object System.Windows.Forms.FolderBrowserDialog
$dialog.Description = 'Select a project folder'
$dialog.ShowNewFolderButton = $true

$owner = New-Object System.Windows.Forms.Form
$owner.ShowInTaskbar = $false
$owner.FormBorderStyle = [System.Windows.Forms.FormBorderStyle]::FixedToolWindow
$owner.StartPosition = [System.Windows.Forms.FormStartPosition]::CenterScreen
$owner.Size = New-Object System.Drawing.Size(1, 1)
$owner.Opacity = 0
$owner.TopMost = $true

try {
    $owner.Show()
    $owner.Activate()
    $owner.BringToFront()
    $result = $dialog.ShowDialog($owner)
} finally {
    $owner.Close()
    $owner.Dispose()
}

if ($result -eq [System.Windows.Forms.DialogResult]::OK) {
    [Console]::Out.Write($dialog.SelectedPath)
}
`

func pickFolder() (string, error) {
	if runtime.GOOS != "windows" {
		return "", errors.New("native folder picker is currently supported on Windows only")
	}
	output, err := exec.Command("powershell", "-NoProfile", "-STA", "-Command", windowsFolderPickerScript).Output()
	if err != nil {
		return "", err
	}
	path := strings.TrimSpace(string(output))
	if path == "" {
		return "", errFolderPickerCancelled
	}
	return path, nil
}
