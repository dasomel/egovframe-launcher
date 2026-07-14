param([string]$Work = "./.work")
New-Item -ItemType Directory -Force -Path $Work | Out-Null
$base = "https://github.com/eGovFramework"
$repos = @(
  "egovframe-boot-sample-java-config",
  "egovframe-template-simple-react",
  "egovframe-template-simple-backend",
  "egovframe-web-sample",
  "egovframe-simple-homepage-template",
  "egovframe-portal-site-template"
)
foreach ($r in $repos) {
  if (Test-Path "$Work/$r/.git") { Write-Host "skip (exists): $r" }
  else { git clone --depth 1 "$base/$r.git" "$Work/$r" }
}
Write-Host "done -> $Work"
