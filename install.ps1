#Requires -Version 5.1
<#
.SYNOPSIS
    dross installer for Windows — the PowerShell analog of install.sh.

.DESCRIPTION
    Bootstraps dross on a fresh Windows machine with no Go toolchain or git checkout.
    Downloads the latest release .zip for this architecture, verifies its SHA-256
    against the release checksums.txt, extracts dross.exe onto a PATH bin dir, then
    runs `dross install` to materialize the slash commands and prompts into ~/.claude.

    Usage:
      irm https://raw.githubusercontent.com/Rivil/dross/main/install.ps1 | iex

    Safety: the binary is staged in a temp dir and moved onto PATH only AFTER the
    SHA-256 verifies (mirroring install.sh), so a failed/interrupted download never
    leaves a partial binary on PATH. Any error aborts (`$ErrorActionPreference`).

    NOTE: authored from the PowerShell/Windows docs and reviewed statically; it has
    NOT been executed on a real Windows host in this phase (no windows/pwsh available
    unattended). The load-bearing self-update logic it mirrors — AssetName / archive
    extraction / SHA-256 verification — is fully unit-tested in Go.

    Env overrides (parity with install.sh):
      DROSS_INSTALL_BASE  asset base URL (default: GitHub latest release download)
      DROSS_API_BASE      GitHub API base (default: https://api.github.com)
      DROSS_VERSION       skip the API lookup and use this tag (e.g. v0.6.0)
      DROSS_BIN_DIR       install dir (default: $HOME\.local\bin)
#>

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocol]::Tls12

$Repo        = 'Rivil/dross'
$ApiBase     = if ($env:DROSS_API_BASE)     { $env:DROSS_API_BASE }     else { 'https://api.github.com' }
$InstallBase = if ($env:DROSS_INSTALL_BASE) { $env:DROSS_INSTALL_BASE } else { "https://github.com/$Repo/releases/latest/download" }
$BinDir      = if ($env:DROSS_BIN_DIR)      { $env:DROSS_BIN_DIR }      else { Join-Path $HOME '.local\bin' }

function Fail($msg) {
    Write-Error "install.ps1: $msg"
    exit 1
}

# Map the Windows processor architecture to a goreleaser arch segment.
function Get-DrossArch {
    $arch = $env:PROCESSOR_ARCHITECTURE
    if (-not $arch) { $arch = (Get-CimInstance Win32_Processor).Architecture }
    switch -Regex ($arch) {
        'AMD64|x86_64|^9$' { return 'amd64' }
        'ARM64|^12$'       { return 'arm64' }
        default            { Fail "unsupported architecture: $arch (dross ships amd64 and arm64)" }
    }
}

# Resolve the release tag, honoring DROSS_VERSION or querying the GitHub API.
function Resolve-Tag {
    if ($env:DROSS_VERSION) { return $env:DROSS_VERSION }
    $rel = Invoke-RestMethod -Uri "$ApiBase/repos/$Repo/releases/latest" -Headers @{ 'Accept' = 'application/vnd.github+json' }
    if (-not $rel.tag_name) { Fail 'could not determine latest release tag' }
    return $rel.tag_name
}

# Abort unless $File's SHA-256 matches its entry in the checksums.txt at $ChecksumsPath.
function Assert-Checksum($File, $AssetName, $ChecksumsPath) {
    $want = $null
    foreach ($line in Get-Content -Path $ChecksumsPath) {
        $fields = ($line -split '\s+') | Where-Object { $_ -ne '' }
        if ($fields.Count -eq 2 -and $fields[1] -eq $AssetName) { $want = $fields[0]; break }
    }
    if (-not $want) { Fail "no checksum entry for $AssetName" }
    $got = (Get-FileHash -Path $File -Algorithm SHA256).Hash
    if ($got -ine $want) { Fail "checksum mismatch for $AssetName (refusing to install)" }
}

function Main {
    $arch = Get-DrossArch
    $tag  = Resolve-Tag
    $version = $tag -replace '^v', ''
    $asset   = "dross_${version}_windows_${arch}.zip"

    $tmp = Join-Path ([IO.Path]::GetTempPath()) ("dross-install-" + [Guid]::NewGuid().ToString('N'))
    New-Item -ItemType Directory -Path $tmp -Force | Out-Null
    try {
        Write-Host "Downloading $asset ($tag)..."
        Invoke-WebRequest -Uri "$InstallBase/$asset"        -OutFile (Join-Path $tmp $asset)          -UseBasicParsing
        Invoke-WebRequest -Uri "$InstallBase/checksums.txt" -OutFile (Join-Path $tmp 'checksums.txt') -UseBasicParsing

        # Verify BEFORE extracting or touching PATH.
        Assert-Checksum -File (Join-Path $tmp $asset) -AssetName $asset -ChecksumsPath (Join-Path $tmp 'checksums.txt')

        Expand-Archive -Path (Join-Path $tmp $asset) -DestinationPath $tmp -Force
        $exe = Join-Path $tmp 'dross.exe'
        if (-not (Test-Path $exe)) { Fail 'release archive did not contain dross.exe' }

        New-Item -ItemType Directory -Path $BinDir -Force | Out-Null
        $dest = Join-Path $BinDir 'dross.exe'
        # Move onto PATH only now — after a verified download and successful extraction.
        Move-Item -Path $exe -Destination $dest -Force
        Write-Host "Installed dross $tag -> $dest"

        # Materialize slash commands + prompts into ~/.claude.
        & $dest install

        # Ensure the bin dir is on the user PATH for future sessions.
        $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
        if (($userPath -split ';') -notcontains $BinDir) {
            [Environment]::SetEnvironmentVariable('Path', "$userPath;$BinDir", 'User')
            Write-Host "Added $BinDir to your user PATH (restart the shell to pick it up)."
        }
        $env:Path = "$env:Path;$BinDir"

        Write-Host ''
        Write-Host "Done. If $BinDir is not on your PATH in this session, add it:"
        Write-Host "  `$env:Path = `"$BinDir;`$env:Path`""
    }
    finally {
        Remove-Item -Path $tmp -Recurse -Force -ErrorAction SilentlyContinue
    }
}

Main
