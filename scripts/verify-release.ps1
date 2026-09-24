<#
.SYNOPSIS
  Verifies a directory of downloaded eGovFrame Launcher release assets.
.DESCRIPTION
  Offline (default): checks SHA256SUMS against every file in the directory.
  -Online: additionally runs `gh attestation verify` against each release
  asset using GitHub's Sigstore-backed attestation API. Requires the GitHub
  CLI (gh) on PATH and authenticated.
.PARAMETER AssetsDir
  Directory containing the downloaded release assets (binaries + SHA256SUMS).
.PARAMETER Online
  Also verify build/SBOM attestations via `gh attestation verify`.
.EXAMPLE
  ./scripts/verify-release.ps1 .\release-assets
.EXAMPLE
  ./scripts/verify-release.ps1 .\release-assets -Online
#>
param(
    [Parameter(Mandatory = $true, Position = 0)]
    [string]$AssetsDir,

    [switch]$Online
)

$ErrorActionPreference = "Stop"
$Repo = "dasomel/egovframe-launcher"

if (-not (Test-Path -LiteralPath $AssetsDir -PathType Container)) {
    Write-Error "not a directory: $AssetsDir"
    exit 2
}

Push-Location $AssetsDir
try {
    if (-not (Test-Path -LiteralPath "SHA256SUMS" -PathType Leaf)) {
        Write-Error "SHA256SUMS not found in $AssetsDir"
        exit 1
    }

    Write-Host "==> Verifying checksums (offline)"
    $failed = $false
    foreach ($line in Get-Content "SHA256SUMS") {
        $line = $line.Trim()
        if ($line -eq "") { continue }
        if ($line -notmatch '^([0-9a-fA-F]{64})\s+\*?(.+)$') { continue }

        $expected = $Matches[1].ToLower()
        $file = $Matches[2]

        if (-not (Test-Path -LiteralPath $file -PathType Leaf)) {
            Write-Warning "missing file: $file"
            $failed = $true
            continue
        }

        $actual = (Get-FileHash -LiteralPath $file -Algorithm SHA256).Hash.ToLower()
        if ($actual -ne $expected) {
            Write-Warning "checksum mismatch: $file"
            $failed = $true
        } else {
            Write-Host "OK: $file"
        }
    }

    if ($failed) {
        Write-Error "checksum verification failed"
        exit 1
    }
    Write-Host "Checksums OK."

    if (-not $Online) {
        Write-Host "Offline verification complete. Pass -Online to also verify build/SBOM attestations."
        exit 0
    }

    if (-not (Get-Command gh -ErrorAction SilentlyContinue)) {
        Write-Error "-Online requires the GitHub CLI (gh), which was not found on PATH"
        exit 1
    }

    Write-Host "==> Verifying attestations (online, repo: $Repo)"
    $attestFailed = $false
    Get-ChildItem -File | Where-Object {
        $_.Name -ne "SHA256SUMS" -and $_.Name -notlike "*.spdx.json"
    } | ForEach-Object {
        Write-Host "--- $($_.Name) ---"
        & gh attestation verify $_.FullName --repo $Repo --source-ref refs/heads/main
        if ($LASTEXITCODE -ne 0) {
            Write-Warning "attestation verification failed for $($_.Name)"
            $attestFailed = $true
        }
    }

    if ($attestFailed) {
        Write-Error "One or more attestation verifications failed."
        exit 1
    }
    Write-Host "All attestation verifications passed."
}
finally {
    Pop-Location
}
