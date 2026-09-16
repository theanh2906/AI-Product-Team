param(
    [switch]$DryRun
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$repositoryRoot = Split-Path -Parent $PSScriptRoot
$tauriConfigPath = Join-Path $repositoryRoot 'src-tauri\tauri.conf.json'
$cargoTomlPath = Join-Path $repositoryRoot 'src-tauri\Cargo.toml'
$cargoLockPath = Join-Path $repositoryRoot 'src-tauri\Cargo.lock'
$buildScriptPath = Join-Path $repositoryRoot 'build.ps1'
$versionPattern = '^(?<major>\d+)\.(?<minor>\d+)\.(?<patch>\d+)$'
$utf8NoBom = [System.Text.UTF8Encoding]::new($false)

function Get-TextFile {
    param([Parameter(Mandatory)][string]$Path)
    return [System.IO.File]::ReadAllText($Path)
}

function Set-TextFile {
    param(
        [Parameter(Mandatory)][string]$Path,
        [Parameter(Mandatory)][string]$Content
    )
    [System.IO.File]::WriteAllText($Path, $Content, $utf8NoBom)
}

function Set-CapturedVersion {
    param(
        [Parameter(Mandatory)][string]$Path,
        [Parameter(Mandatory)][string]$Pattern,
        [Parameter(Mandatory)][string]$ExpectedVersion,
        [Parameter(Mandatory)][string]$NextVersion,
        [System.Text.RegularExpressions.RegexOptions]$Options = [System.Text.RegularExpressions.RegexOptions]::None
    )

    $content = Get-TextFile -Path $Path
    $match = [System.Text.RegularExpressions.Regex]::Match($content, $Pattern, $Options)
    if (-not $match.Success) {
        throw "Could not find a version field in $Path."
    }

    $capturedVersion = $match.Groups['version'].Value
    if ($capturedVersion -ne $ExpectedVersion) {
        throw "Version mismatch in $Path. Expected $ExpectedVersion but found $capturedVersion."
    }

    $updated = $content.Substring(0, $match.Groups['version'].Index) +
        $NextVersion +
        $content.Substring($match.Groups['version'].Index + $match.Groups['version'].Length)

    if (-not $DryRun) {
        Set-TextFile -Path $Path -Content $updated
    }
}

foreach ($path in @($tauriConfigPath, $cargoTomlPath, $cargoLockPath, $buildScriptPath)) {
    if (-not (Test-Path -LiteralPath $path -PathType Leaf)) {
        throw "Required version file was not found: $path"
    }
}

$tauriConfig = Get-Content -LiteralPath $tauriConfigPath -Raw | ConvertFrom-Json
$currentVersion = [string]$tauriConfig.version
$versionMatch = [System.Text.RegularExpressions.Regex]::Match($currentVersion, $versionPattern)
if (-not $versionMatch.Success) {
    throw "Only stable semantic versions can be auto-bumped. Current version: $currentVersion"
}

$nextVersion = '{0}.{1}.{2}' -f
    $versionMatch.Groups['major'].Value,
    $versionMatch.Groups['minor'].Value,
    ([int]$versionMatch.Groups['patch'].Value + 1)

$multiline = [System.Text.RegularExpressions.RegexOptions]::Multiline
$singleline = [System.Text.RegularExpressions.RegexOptions]::Singleline
Set-CapturedVersion `
    -Path $tauriConfigPath `
    -Pattern '(?m)(?<prefix>^\s*"version"\s*:\s*")(?<version>\d+\.\d+\.\d+)(?<suffix>"\s*,)' `
    -ExpectedVersion $currentVersion `
    -NextVersion $nextVersion `
    -Options $multiline
Set-CapturedVersion `
    -Path $cargoTomlPath `
    -Pattern '(?s)(?<prefix>\[package\].*?^\s*version\s*=\s*")(?<version>\d+\.\d+\.\d+)(?<suffix>")' `
    -ExpectedVersion $currentVersion `
    -NextVersion $nextVersion `
    -Options ($singleline -bor $multiline)
Set-CapturedVersion `
    -Path $cargoLockPath `
    -Pattern '(?s)(?<prefix>name = "productcrew-desktop"\r?\nversion = ")(?<version>\d+\.\d+\.\d+)(?<suffix>")' `
    -ExpectedVersion $currentVersion `
    -NextVersion $nextVersion `
    -Options $singleline
Set-CapturedVersion `
    -Path $buildScriptPath `
    -Pattern "(?m)(?<prefix>^\`$version\s*=\s*')(?<version>\d+\.\d+\.\d+)(?<suffix>')" `
    -ExpectedVersion $currentVersion `
    -NextVersion $nextVersion `
    -Options $multiline

if ($DryRun) {
    Write-Host "ProductCrew version would bump: $currentVersion -> $nextVersion" -ForegroundColor Cyan
} else {
    Write-Host "ProductCrew version bumped: $currentVersion -> $nextVersion" -ForegroundColor Cyan
}
