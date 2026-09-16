$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$installer = $false
$showHelp = $false
$cleanDistribution = $true
$version = '0.1.145'
$versionPattern = '^\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.-]+)?$'

for ($index = 0; $index -lt $args.Count; $index++) {
    switch ($args[$index]) {
        '--installer' { $installer = $true }
        '--no-clean-dist' { $cleanDistribution = $false }
        '--version' {
            $index++
            if ($index -ge $args.Count -or $args[$index] -notmatch $versionPattern) {
                throw '--version requires a semantic version such as 1.2.3.'
            }
            $version = $args[$index]
        }
        '--help' { $showHelp = $true }
        '-h' { $showHelp = $true }
        default { throw "Unknown build option '$($args[$index])'. Run .\build.ps1 --help for usage." }
    }
}

if ($showHelp) {
    @'
ProductCrew desktop packaging

Usage:
  .\build.ps1                         Build the portable Tauri package (default)
  .\build.ps1 --installer             Build the Tauri NSIS installer
  .\build.ps1 --no-clean-dist         Keep existing dist artifacts before building
  .\build.ps1 --version 1.2.3         Override the artifact version
  .\build.ps1 --installer --version 1.2.3

By default, dist is cleaned before each build. Release builds call the
installer step with --no-clean-dist so portable and installer artifacts
can be published together.
'@ | Write-Host
    exit 0
}

$repositoryRoot = $PSScriptRoot
$tauriCli = Join-Path $repositoryRoot 'frontend\node_modules\.bin\tauri.cmd'
$releaseRoot = Join-Path $repositoryRoot 'src-tauri\target\release'
$distributionRoot = Join-Path $repositoryRoot 'dist'
$productName = 'ProductCrew'
$architecture = 'x64'
$configOverridePath = Join-Path $distributionRoot '.tauri-build-config.json'
$defaultUpdatePath = 'C:\Tools\updates'

function Get-UpdatePath {
    $envUpdatePath = $env:PRODUCTCREW_UPDATE_DIR
    if (-not [string]::IsNullOrWhiteSpace($envUpdatePath)) {
        if (-not [System.IO.Path]::IsPathFullyQualified($envUpdatePath)) {
            throw "PRODUCTCREW_UPDATE_DIR must be an absolute path: $envUpdatePath"
        }
        return $envUpdatePath
    }

    $settingsPath = Join-Path $env:USERPROFILE '.productcrew\settings.json'
    if (-not (Test-Path -LiteralPath $settingsPath -PathType Leaf)) { return $defaultUpdatePath }
    try {
        $settings = Get-Content -LiteralPath $settingsPath -Raw | ConvertFrom-Json
        $configured = if ($settings.PSObject.Properties['updatePath']) { $settings.updatePath } else { $null }
        if ($configured -and [System.IO.Path]::IsPathFullyQualified($configured)) { return $configured }
    } catch {
        Write-Warning "Could not read ProductCrew updatePath from $settingsPath; using $defaultUpdatePath."
    }
    return $defaultUpdatePath
}

function Assert-Command {
    param([Parameter(Mandatory)][string]$Name)
    if (-not (Get-Command $Name -ErrorAction SilentlyContinue)) {
        throw "Required build tool '$Name' was not found in PATH."
    }
}

function Invoke-NativeCommand {
    param(
        [Parameter(Mandatory)][string]$Executable,
        [Parameter(ValueFromRemainingArguments)][string[]]$Arguments
    )
    & $Executable @Arguments
    if ($LASTEXITCODE -ne 0) {
        throw "Command failed with exit code ${LASTEXITCODE}: $Executable $($Arguments -join ' ')"
    }
}

function Reset-DistributionRoot {
    New-Item -ItemType Directory -Path $distributionRoot -Force | Out-Null

    $resolvedRepositoryRoot = (Resolve-Path -LiteralPath $repositoryRoot).Path
    $resolvedDistributionRoot = (Resolve-Path -LiteralPath $distributionRoot).Path
    if (-not $resolvedDistributionRoot.StartsWith($resolvedRepositoryRoot, [System.StringComparison]::OrdinalIgnoreCase)) {
        throw "Refusing to clean dist outside the repository: $resolvedDistributionRoot"
    }
    if ((Split-Path -Leaf $resolvedDistributionRoot) -ne 'dist') {
        throw "Refusing to clean unexpected distribution folder: $resolvedDistributionRoot"
    }

    Get-ChildItem -LiteralPath $resolvedDistributionRoot -Force | ForEach-Object {
        Remove-Item -LiteralPath $_.FullName -Recurse -Force
    }
    Write-Host "Cleaned dist folder: $resolvedDistributionRoot" -ForegroundColor DarkCyan
}

function Publish-GoRuntimeArtifact {
    $runtimeBinary = Get-ChildItem -LiteralPath (Join-Path $repositoryRoot 'src-tauri\binaries') -Filter 'productcrew-server-*.exe' |
        Sort-Object LastWriteTime -Descending |
        Select-Object -First 1
    if (-not $runtimeBinary) {
        throw 'The Go server runtime binary was not produced under src-tauri\binaries.'
    }

    $runtimeArtifact = Join-Path $distributionRoot "$productName-v$version-windows-$architecture-server.exe"
    Copy-Item -LiteralPath $runtimeBinary.FullName -Destination $runtimeArtifact -Force
    Write-Host "Server runtime artifact: $runtimeArtifact" -ForegroundColor DarkCyan
    return $runtimeArtifact
}

Assert-Command 'node'
Assert-Command 'npm'
Assert-Command 'go'
Assert-Command 'rustc'
Assert-Command 'cargo'

if (-not (Test-Path -LiteralPath $tauriCli -PathType Leaf)) {
    throw "Tauri CLI is not installed. Run 'npm install' in the frontend folder first."
}

$runningRelease = Get-Process -Name 'productcrew-desktop' -ErrorAction SilentlyContinue |
    Where-Object { $_.Path -eq (Join-Path $releaseRoot 'productcrew-desktop.exe') }
if ($runningRelease) {
    throw 'The release desktop app is running. Choose Quit from its system tray menu before building again.'
}

if ($cleanDistribution) {
    Reset-DistributionRoot
} else {
    New-Item -ItemType Directory -Path $distributionRoot -Force | Out-Null
}
@{ version = $version } | ConvertTo-Json -Compress | Set-Content -LiteralPath $configOverridePath -Encoding utf8NoBOM
$previousLinker = $env:CARGO_TARGET_X86_64_PC_WINDOWS_MSVC_LINKER
$previousJobs = $env:CARGO_BUILD_JOBS
$previousProductVersion = $env:PRODUCTCREW_VERSION
$env:CARGO_TARGET_X86_64_PC_WINDOWS_MSVC_LINKER = 'rust-lld.exe'
$env:CARGO_BUILD_JOBS = '2'
$env:PRODUCTCREW_VERSION = $version

try {
    Push-Location $repositoryRoot
    try {
        if ($installer) {
            Write-Host '[1/2] Building Angular, embedded Go runtime, Tauri shell, and NSIS installer...' -ForegroundColor Cyan
            Invoke-NativeCommand $tauriCli 'build' '--bundles' 'nsis' '--features' 'installer-updates' '--config' $configOverridePath

            Write-Host '[2/2] Publishing installer artifact...' -ForegroundColor Cyan
            $sourceArtifact = Get-ChildItem -LiteralPath (Join-Path $releaseRoot 'bundle\nsis') -Filter '*-setup.exe' |
                Sort-Object LastWriteTime -Descending |
                Select-Object -First 1
            if (-not $sourceArtifact) {
                throw 'Tauri completed without producing an NSIS setup executable.'
            }
            $artifact = Join-Path $distributionRoot "$productName-v$version-windows-$architecture-setup.exe"
            Copy-Item -LiteralPath $sourceArtifact.FullName -Destination $artifact -Force
            Publish-GoRuntimeArtifact | Out-Null
            $updatePath = Get-UpdatePath
            New-Item -ItemType Directory -Path $updatePath -Force | Out-Null
            $publishedInstaller = Join-Path $updatePath (Split-Path -Leaf $artifact)
            Copy-Item -LiteralPath $artifact -Destination $publishedInstaller -Force
            $publishedFile = Get-Item -LiteralPath $publishedInstaller
            $publishedHash = Get-FileHash -LiteralPath $publishedInstaller -Algorithm SHA256
            $manifest = [ordered]@{
                version = $version
                installer = $publishedFile.Name
                sha256 = $publishedHash.Hash
                size = $publishedFile.Length
                notes = if ([string]::IsNullOrWhiteSpace($env:PRODUCTCREW_UPDATE_NOTES)) { "ProductCrew $version installer update" } else { $env:PRODUCTCREW_UPDATE_NOTES }
                publishedAt = [DateTime]::UtcNow.ToString('o')
            }
            $manifest | ConvertTo-Json | Set-Content -LiteralPath (Join-Path $updatePath 'installer-latest.json') -Encoding utf8NoBOM
            Write-Host "Update channel published: $updatePath" -ForegroundColor DarkCyan
        } else {
            Write-Host '[1/2] Building Angular, embedded Go runtime, and portable Tauri shell...' -ForegroundColor Cyan
            Invoke-NativeCommand $tauriCli 'build' '--no-bundle' '--config' $configOverridePath

            Write-Host '[2/2] Creating portable ZIP...' -ForegroundColor Cyan
            $packageName = "$productName-v$version-windows-$architecture-portable"
            $packageRoot = Join-Path $distributionRoot $packageName
            $artifact = Join-Path $distributionRoot "$packageName.zip"
            $desktopExe = Join-Path $releaseRoot 'productcrew-desktop.exe'
            if (-not (Test-Path -LiteralPath $desktopExe -PathType Leaf)) {
                throw 'Tauri completed without producing the desktop executable.'
            }
            if (Test-Path -LiteralPath $packageRoot) { Remove-Item -LiteralPath $packageRoot -Recurse -Force }
            if (Test-Path -LiteralPath $artifact) { Remove-Item -LiteralPath $artifact -Force }
            New-Item -ItemType Directory -Path $packageRoot | Out-Null
            Copy-Item -LiteralPath $desktopExe -Destination (Join-Path $packageRoot "$productName.exe")
            $serverArtifact = Publish-GoRuntimeArtifact
            Copy-Item -LiteralPath $serverArtifact -Destination (Join-Path $packageRoot 'productcrew-server.exe')
            Copy-Item -LiteralPath (Join-Path $repositoryRoot 'packaging\windows\PORTABLE-README.txt') -Destination (Join-Path $packageRoot 'README.txt')
            Compress-Archive -Path (Join-Path $packageRoot '*') -DestinationPath $artifact -CompressionLevel Optimal
        }
    } finally {
        Pop-Location
    }
} finally {
    $env:CARGO_TARGET_X86_64_PC_WINDOWS_MSVC_LINKER = $previousLinker
    $env:CARGO_BUILD_JOBS = $previousJobs
    $env:PRODUCTCREW_VERSION = $previousProductVersion
    if (Test-Path -LiteralPath $configOverridePath) { Remove-Item -LiteralPath $configOverridePath -Force }
}

Write-Host "Build complete: $artifact" -ForegroundColor Green
