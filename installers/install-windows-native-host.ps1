param(
    [Parameter()]
    [ValidatePattern('^[a-p]{32}$')]
    [string]$ChromeExtensionId,

    [Parameter()]
    [ValidatePattern('^[a-p]{32}$')]
    [string]$EdgeExtensionId,

    [switch]$Upgrade
)

$ErrorActionPreference = 'Stop'

if (-not $ChromeExtensionId -and -not $EdgeExtensionId) {
    throw 'Provide -ChromeExtensionId, -EdgeExtensionId, or both. Copy each ID from that browser''s extensions page.'
}
if (-not $env:LOCALAPPDATA) {
    throw 'LOCALAPPDATA is not set; run this script from your signed-in Windows account.'
}
if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    throw 'Go 1.23 or newer is required. Install Go, reopen PowerShell, and retry.'
}

$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$installDir = Join-Path $env:LOCALAPPDATA 'Programs\BrightspaceCLI'
$cliPath = Join-Path $installDir 'd2l.exe'
$registrations = @()

if ($ChromeExtensionId) {
    $registrations += [pscustomobject]@{
        Browser = 'Chrome'
        ExtensionId = $ChromeExtensionId
        HostPath = Join-Path $installDir 'd2l-native-host-chrome.exe'
        ManifestPath = Join-Path $installDir 'com.sait.d2l.chrome.json'
        RegistryPath = 'HKCU:\Software\Google\Chrome\NativeMessagingHosts\com.sait.d2l'
    }
}
if ($EdgeExtensionId) {
    $registrations += [pscustomobject]@{
        Browser = 'Edge'
        ExtensionId = $EdgeExtensionId
        HostPath = Join-Path $installDir 'd2l-native-host-edge.exe'
        ManifestPath = Join-Path $installDir 'com.sait.d2l.edge.json'
        RegistryPath = 'HKCU:\Software\Microsoft\Edge\NativeMessagingHosts\com.sait.d2l'
    }
}

function Get-GoBuildMetadata([string]$Path) {
    if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) { return '' }
    $metadata = & go version -m $Path 2>&1 | Out-String
    if ($LASTEXITCODE -ne 0) { return '' }
    return $metadata
}

function Assert-ManagedBinary([string]$Path, [string]$ExtensionId) {
    $metadata = Get-GoBuildMetadata $Path
    if ($metadata -notmatch '(?m)^[\t ]*path[\t ]+d2l-cli[\t ]*$') {
        throw "Refusing to replace an unrecognized binary: $Path"
    }
    if ($ExtensionId -and -not $metadata.Contains("chrome-extension://$ExtensionId/")) {
        throw "Refusing to replace a native host bound to another extension ID: $Path"
    }
}

function Assert-ManagedManifest($Registration) {
    if (-not (Test-Path -LiteralPath $Registration.ManifestPath -PathType Leaf)) { return }
    $manifest = Get-Content -Raw -LiteralPath $Registration.ManifestPath | ConvertFrom-Json
    if ($manifest.name -ne 'com.sait.d2l' -or
        [IO.Path]::GetFullPath($manifest.path) -ne [IO.Path]::GetFullPath($Registration.HostPath) -or
        $manifest.allowed_origins -notcontains "chrome-extension://$($Registration.ExtensionId)/") {
        throw "Refusing to replace an unrecognized native host manifest: $($Registration.ManifestPath)"
    }
}

function Assert-ManagedRegistration($Registration) {
    if (-not (Test-Path -LiteralPath $Registration.RegistryPath)) { return }
    if (-not $Upgrade) { throw "Native Messaging registration already exists: $($Registration.RegistryPath). Use -Upgrade for a managed install." }
    $registeredPath = (Get-Item -LiteralPath $Registration.RegistryPath).GetValue('')
    if (-not $registeredPath -or [IO.Path]::GetFullPath($registeredPath) -ne [IO.Path]::GetFullPath($Registration.ManifestPath)) {
        throw "Refusing to replace an unrecognized Native Messaging registration: $($Registration.RegistryPath)"
    }
    if (-not (Test-Path -LiteralPath $Registration.ManifestPath -PathType Leaf)) {
        throw "Registered manifest is missing: $($Registration.ManifestPath)"
    }
    Assert-ManagedManifest $Registration
}

$filesToCheck = @()
if (Test-Path -LiteralPath $cliPath) { $filesToCheck += [pscustomobject]@{ Path = $cliPath; Id = '' } }
foreach ($registration in $registrations) {
    if (Test-Path -LiteralPath $registration.HostPath) {
        $filesToCheck += [pscustomobject]@{ Path = $registration.HostPath; Id = $registration.ExtensionId }
    }
    if (Test-Path -LiteralPath $registration.ManifestPath) {
        if (-not $Upgrade) { throw "Native host manifest already exists: $($registration.ManifestPath). Use -Upgrade for a managed install." }
        Assert-ManagedManifest $registration
    }
    Assert-ManagedRegistration $registration
}
if ($filesToCheck.Count -gt 0 -and -not $Upgrade) {
    throw 'An installation already exists. Use -Upgrade to replace files managed by this installer.'
}
foreach ($file in $filesToCheck) { Assert-ManagedBinary $file.Path $file.Id }

New-Item -ItemType Directory -Force -Path $installDir | Out-Null
$buildDir = Join-Path ([IO.Path]::GetTempPath()) ("d2l-cli-build-" + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $buildDir | Out-Null
try {
    Push-Location $repoRoot
    try {
        $cliBuild = Join-Path $buildDir 'd2l.exe'
        & go build -o $cliBuild .
        if ($LASTEXITCODE -ne 0) { throw 'Building d2l.exe failed.' }
        foreach ($registration in $registrations) {
            $hostBuild = Join-Path $buildDir ("d2l-native-host-$($registration.Browser.ToLowerInvariant()).exe")
            $ldflags = "-X main.expectedExtensionOrigin=chrome-extension://$($registration.ExtensionId)/"
            & go build -ldflags $ldflags -o $hostBuild .
            if ($LASTEXITCODE -ne 0) { throw "Building the $($registration.Browser) native host failed." }
            $registration | Add-Member -NotePropertyName BuildPath -NotePropertyValue $hostBuild
        }
    }
    finally { Pop-Location }

    $backupStamp = Get-Date -Format 'yyyyMMdd-HHmmss'
    $pathsToBackup = @($cliPath) + @($registrations | ForEach-Object { $_.HostPath; $_.ManifestPath })
    foreach ($path in $pathsToBackup) {
        if (Test-Path -LiteralPath $path -PathType Leaf) {
            if (-not $Upgrade) { throw "Refusing to overwrite existing file: $path" }
            Copy-Item -LiteralPath $path -Destination "$path.backup.$backupStamp" -ErrorAction Stop
        }
    }

    Copy-Item -LiteralPath $cliBuild -Destination $cliPath -Force
    foreach ($registration in $registrations) {
        Copy-Item -LiteralPath $registration.BuildPath -Destination $registration.HostPath -Force
        $manifest = [ordered]@{
            name = 'com.sait.d2l'
            description = 'D2L read-only CLI native messaging host'
            path = $registration.HostPath
            type = 'stdio'
            allowed_origins = @("chrome-extension://$($registration.ExtensionId)/")
        }
        $manifestJson = $manifest | ConvertTo-Json -Depth 4
        $utf8NoBom = New-Object System.Text.UTF8Encoding -ArgumentList $false
        [IO.File]::WriteAllText($registration.ManifestPath, $manifestJson, $utf8NoBom)
        New-Item -Path $registration.RegistryPath -Force | Out-Null
        New-ItemProperty -Path $registration.RegistryPath -Name '(default)' -Value $registration.ManifestPath -PropertyType String -Force | Out-Null
    }
}
finally {
    Remove-Item -LiteralPath $buildDir -Recurse -Force -ErrorAction SilentlyContinue
}

Write-Host "Installed CLI: $cliPath"
foreach ($registration in $registrations) {
    Write-Host "Registered $($registration.Browser) native host for extension $($registration.ExtensionId)."
}
if ($Upgrade) { Write-Host "Existing managed files were backed up with suffix .backup.$backupStamp" }
Write-Host 'Restart the selected browser(s), then use Test Native Messaging in the extension.'
Write-Host "Run the CLI with: & `"$cliPath`" status"
