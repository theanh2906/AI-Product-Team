param(
    [string]$Version = '',
    [string]$ReleaseNotes = ''
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$repositoryRoot = Split-Path -Parent $PSScriptRoot
$buildScriptPath = Join-Path $repositoryRoot 'build.ps1'
$publishDir = if ([string]::IsNullOrWhiteSpace($env:PRODUCTCREW_UPDATE_DIR)) {
    Join-Path $repositoryRoot 'dist\r2-publish'
} else {
    $env:PRODUCTCREW_UPDATE_DIR
}
$r2Endpoint = if ([string]::IsNullOrWhiteSpace($env:R2_ENDPOINT)) {
    'https://8b8d3182a90c830065dcb9563bd9c9f8.r2.cloudflarestorage.com'
} else {
    $env:R2_ENDPOINT
}
$r2Bucket = if ([string]::IsNullOrWhiteSpace($env:R2_BUCKET)) { 'installers' } else { $env:R2_BUCKET }
$r2Prefix = if ([string]::IsNullOrWhiteSpace($env:R2_PREFIX)) { 'ProductCrew' } else { $env:R2_PREFIX.Trim('/') }
$publicBaseUrl = if ([string]::IsNullOrWhiteSpace($env:PRODUCTCREW_INSTALLER_PUBLIC_BASE_URL)) {
    'https://installers.bennasolutions.com/ProductCrew'
} else {
    $env:PRODUCTCREW_INSTALLER_PUBLIC_BASE_URL.TrimEnd('/')
}

function Assert-Command {
    param([Parameter(Mandatory)][string]$Name)
    if (-not (Get-Command $Name -ErrorAction SilentlyContinue)) {
        throw "Required deploy tool '$Name' was not found in PATH."
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

function Assert-SafeR2Target {
    if ([string]::IsNullOrWhiteSpace($r2Bucket)) {
        throw 'R2_BUCKET cannot be empty.'
    }
    if ([string]::IsNullOrWhiteSpace($r2Prefix) -or $r2Prefix -eq '.' -or $r2Prefix -eq '..') {
        throw 'R2_PREFIX must point to the ProductCrew folder before cleanup can run.'
    }
    if ($r2Prefix -match '(^|/)\.\.($|/)' -or $r2Prefix -match '[\\*?]') {
        throw "Unsafe R2_PREFIX: $r2Prefix"
    }
}

function Resolve-AccessKey {
    if (-not [string]::IsNullOrWhiteSpace($env:AWS_ACCESS_KEY_ID)) { return $env:AWS_ACCESS_KEY_ID }
    if (-not [string]::IsNullOrWhiteSpace($env:R2_ACCESS_KEY_ID)) { return $env:R2_ACCESS_KEY_ID }
    return ''
}

function Resolve-SecretKey {
    if (-not [string]::IsNullOrWhiteSpace($env:AWS_SECRET_ACCESS_KEY)) { return $env:AWS_SECRET_ACCESS_KEY }
    if (-not [string]::IsNullOrWhiteSpace($env:R2_SECRET_ACCESS_KEY)) { return $env:R2_SECRET_ACCESS_KEY }
    return ''
}

if (-not (Test-Path -LiteralPath $buildScriptPath -PathType Leaf)) {
    throw "Build script was not found: $buildScriptPath"
}
if (-not [System.IO.Path]::IsPathFullyQualified($publishDir)) {
    throw "PRODUCTCREW_UPDATE_DIR must be an absolute path: $publishDir"
}
Assert-SafeR2Target
Assert-Command 'aws'

$accessKey = Resolve-AccessKey
$secretKey = Resolve-SecretKey
if ([string]::IsNullOrWhiteSpace($accessKey) -or [string]::IsNullOrWhiteSpace($secretKey)) {
    throw 'Missing R2 credentials. Set R2_ACCESS_KEY_ID and R2_SECRET_ACCESS_KEY, or AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY.'
}

$previousUpdateDir = $env:PRODUCTCREW_UPDATE_DIR
$previousNotes = $env:PRODUCTCREW_UPDATE_NOTES
$previousAccessKey = $env:AWS_ACCESS_KEY_ID
$previousSecretKey = $env:AWS_SECRET_ACCESS_KEY
$previousRegion = $env:AWS_DEFAULT_REGION
$previousMetadataDisabled = $env:AWS_EC2_METADATA_DISABLED

try {
    $env:PRODUCTCREW_UPDATE_DIR = $publishDir
    if (-not [string]::IsNullOrWhiteSpace($ReleaseNotes)) {
        $env:PRODUCTCREW_UPDATE_NOTES = $ReleaseNotes
    }

    $buildArgs = @('--installer')
    if (-not [string]::IsNullOrWhiteSpace($Version)) {
        $buildArgs += @('--version', $Version)
    }
    & $buildScriptPath @buildArgs
    if ($LASTEXITCODE -ne 0) {
        throw "ProductCrew installer build failed with exit code $LASTEXITCODE."
    }

    $manifestPath = Join-Path $publishDir 'installer-latest.json'
    if (-not (Test-Path -LiteralPath $manifestPath -PathType Leaf)) {
        throw "Missing installer manifest: $manifestPath"
    }

    $manifest = Get-Content -LiteralPath $manifestPath -Raw | ConvertFrom-Json
    $installerName = [string]$manifest.installer
    if ([string]::IsNullOrWhiteSpace($installerName) -or $installerName -match '[\\/]') {
        throw 'installer-latest.json must reference one installer file in the publish folder.'
    }

    $installerPath = Join-Path $publishDir $installerName
    if (-not (Test-Path -LiteralPath $installerPath -PathType Leaf)) {
        throw "Missing installer artifact referenced by manifest: $installerPath"
    }

    $installerFile = Get-Item -LiteralPath $installerPath
    if ($installerFile.Length -ne [int64]$manifest.size) {
        throw "Installer size mismatch: manifest=$($manifest.size), file=$($installerFile.Length)."
    }

    $actualHash = (Get-FileHash -LiteralPath $installerPath -Algorithm SHA256).Hash
    if (-not $actualHash.Equals([string]$manifest.sha256, [System.StringComparison]::OrdinalIgnoreCase)) {
        throw 'Installer SHA-256 does not match installer-latest.json.'
    }

    $env:AWS_ACCESS_KEY_ID = $accessKey
    $env:AWS_SECRET_ACCESS_KEY = $secretKey
    $env:AWS_DEFAULT_REGION = if ([string]::IsNullOrWhiteSpace($env:AWS_DEFAULT_REGION)) { 'auto' } else { $env:AWS_DEFAULT_REGION }
    $env:AWS_EC2_METADATA_DISABLED = 'true'

    $prefixUri = "s3://$r2Bucket/$r2Prefix"
    Write-Host "Replacing ProductCrew installer package at $prefixUri/" -ForegroundColor Cyan
    Invoke-NativeCommand aws 's3' 'rm' "$prefixUri/" '--recursive' '--endpoint-url' $r2Endpoint '--only-show-errors'
    Invoke-NativeCommand aws 's3' 'cp' $installerPath "$prefixUri/$installerName" '--endpoint-url' $r2Endpoint '--content-type' 'application/octet-stream' '--no-progress'
    Invoke-NativeCommand aws 's3' 'cp' $manifestPath "$prefixUri/installer-latest.json" '--endpoint-url' $r2Endpoint '--content-type' 'application/json' '--cache-control' 'no-cache' '--no-progress'

    Write-Host "Published ProductCrew installer: $publicBaseUrl/$installerName" -ForegroundColor Green
    Write-Host "Published ProductCrew manifest: $publicBaseUrl/installer-latest.json" -ForegroundColor Green
} finally {
    $env:PRODUCTCREW_UPDATE_DIR = $previousUpdateDir
    $env:PRODUCTCREW_UPDATE_NOTES = $previousNotes
    $env:AWS_ACCESS_KEY_ID = $previousAccessKey
    $env:AWS_SECRET_ACCESS_KEY = $previousSecretKey
    $env:AWS_DEFAULT_REGION = $previousRegion
    $env:AWS_EC2_METADATA_DISABLED = $previousMetadataDisabled
}
