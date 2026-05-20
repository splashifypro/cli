# splashify CLI installer for Windows (PowerShell).
#
# Usage:
#   iwr -useb https://raw.githubusercontent.com/splashifypro/cli/main/install.ps1 | iex
#
# Environment overrides:
#   $env:SPLASHIFY_VERSION       e.g. v0.1.0 (default: latest release)
#   $env:SPLASHIFY_INSTALL_DIR   default: $env:USERPROFILE\.splashify\bin

$ErrorActionPreference = 'Stop'

$Repo = 'splashifypro/cli'

function Say  ($msg) { Write-Host $msg }
function Warn ($msg) { Write-Host $msg -ForegroundColor Yellow }
function Die  ($msg) { Write-Error $msg; exit 1 }

# ── 1. Detect arch ────────────────────────────────────────────────────────────
$arch = $env:PROCESSOR_ARCHITECTURE
switch ($arch) {
  'AMD64' { $goarch = 'x86_64' }
  'x86_64' { $goarch = 'x86_64' }
  default { Die "unsupported architecture: $arch (only x86_64 is published for Windows)" }
}

# ── 2. Resolve version ───────────────────────────────────────────────────────
$version = $env:SPLASHIFY_VERSION
if (-not $version) {
  Say 'Looking up latest splashify release...'
  $headers = @{ 'User-Agent' = 'splashify-installer' }
  $release = Invoke-RestMethod -Headers $headers `
    -Uri "https://api.github.com/repos/$Repo/releases/latest"
  if (-not $release.tag_name) { Die 'no published splashify CLI release found' }
  $version = $release.tag_name
}
if ($version -notmatch '^v') { $version = "v$version" }
$tag = $version
# GoReleaser names assets with the bare semver ({{ .Version }}, no `v` prefix)
# while the release tag keeps the `v`. Keep both forms.
$assetVersion = $version -replace '^v',''
Say "Installing splashify $version for windows/$goarch"

# ── 3. Download artefacts ────────────────────────────────────────────────────
$asset = "splashify_${assetVersion}_windows_${goarch}.zip"
$base  = "https://github.com/$Repo/releases/download/$tag"
$tmp   = Join-Path $env:TEMP "splashify-install-$([Guid]::NewGuid())"
New-Item -ItemType Directory -Force -Path $tmp | Out-Null

try {
  Say "Downloading $asset..."
  Invoke-WebRequest -UseBasicParsing -Uri "$base/$asset"     -OutFile (Join-Path $tmp $asset)
  Invoke-WebRequest -UseBasicParsing -Uri "$base/SHA256SUMS" -OutFile (Join-Path $tmp 'SHA256SUMS')

  # ── 4. Verify checksum ────────────────────────────────────────────────────
  $expectedLine = (Get-Content (Join-Path $tmp 'SHA256SUMS')) `
    | Where-Object { $_ -match "  $([Regex]::Escape($asset))$" } `
    | Select-Object -First 1
  if (-not $expectedLine) { Die "no SHA256SUMS entry for $asset" }
  $expected = ($expectedLine -split '\s+')[0]
  $actual   = (Get-FileHash -Algorithm SHA256 -Path (Join-Path $tmp $asset)).Hash.ToLower()
  if ($actual -ne $expected.ToLower()) {
    Die "checksum verification failed for $asset"
  }

  # ── 5. Extract and install ────────────────────────────────────────────────
  Expand-Archive -LiteralPath (Join-Path $tmp $asset) -DestinationPath $tmp -Force
  $binarySrc = Join-Path $tmp 'splashify.exe'
  if (-not (Test-Path $binarySrc)) { Die 'splashify.exe not found in archive' }

  $destDir = $env:SPLASHIFY_INSTALL_DIR
  if (-not $destDir) { $destDir = Join-Path $env:USERPROFILE '.splashify\bin' }
  New-Item -ItemType Directory -Force -Path $destDir | Out-Null
  $destExe = Join-Path $destDir 'splashify.exe'

  Say "Installing to $destExe"
  Move-Item -Force -Path $binarySrc -Destination $destExe

  # ── 6. PATH hint + next step ──────────────────────────────────────────────
  $pathEntries = ($env:PATH -split ';') | Where-Object { $_ -ne '' }
  if ($pathEntries -notcontains $destDir) {
    Warn ''
    Warn "$destDir is not on your PATH for this session."
    Warn 'Add it permanently:'
    Warn "  setx PATH ""`$env:PATH;$destDir"""
    Warn '(open a new terminal afterwards to pick up the change)'
  }

  Say ''
  Say '[OK] splashify installed.'
  Say '     Next: run "splashify connect" and paste an access token from'
  Say '           Splashify Pro -> Settings -> Developer -> Access Tokens.'
}
finally {
  Remove-Item -Recurse -Force -ErrorAction SilentlyContinue $tmp
}
