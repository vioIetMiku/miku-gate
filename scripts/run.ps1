param(
 [ValidateSet('build','test','init','serve','doctor')][string]$Action = 'serve',
 [string]$Data = '.data',
 [string]$Listen = '127.0.0.1:3939'
)
$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
Set-Location -LiteralPath $root
$go = Join-Path $root '.tools/go/bin/go.exe'
if (-not (Test-Path -LiteralPath $go)) { $go = (Get-Command go -ErrorAction Stop).Source }
$env:GOPATH = Join-Path $root '.cache/gopath'
$env:GOCACHE = Join-Path $root '.cache/go-build'
$env:GOTELEMETRY = 'off'
$binary = Join-Path $root 'bin/miku-gate.exe'
function Build-App {
 Push-Location (Join-Path $root 'web')
 try { & npm run build; if ($LASTEXITCODE -ne 0) { throw '前端构建失败' } } finally { Pop-Location }
 New-Item -ItemType Directory -Path (Join-Path $root 'bin') -Force | Out-Null
 & $go build -trimpath -o $binary ./cmd/miku-gate
 if ($LASTEXITCODE -ne 0) { throw 'Go 构建失败' }
}
switch ($Action) {
 'build' { Build-App }
 'test' {
  & $go test ./...; if ($LASTEXITCODE -ne 0) { throw 'Go 测试失败' }
  Push-Location (Join-Path $root 'web')
  try { & npm test; if ($LASTEXITCODE -ne 0) { throw '前端测试失败' } } finally { Pop-Location }
 }
 default {
  if (-not (Test-Path -LiteralPath $binary)) { Build-App }
  if ($Action -eq 'serve') { & $binary serve --data $Data --listen $Listen }
  elseif ($Action -eq 'init') { & $binary init --data $Data }
  else { & $binary doctor }
  if ($LASTEXITCODE -ne 0) { throw '命令执行失败' }
 }
}
