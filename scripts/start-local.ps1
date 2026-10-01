param(
 [Parameter(Mandatory=$true)][string]$RuntimeRoot,
 [string]$StateRoot = './runtime/native'
)
$ErrorActionPreference = 'Stop'
$projectRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$runtimePath = (Resolve-Path -LiteralPath $RuntimeRoot).Path
$statePath = [IO.Path]::GetFullPath($StateRoot)
New-Item -ItemType Directory -Force -Path $statePath | Out-Null
$goPath = Join-Path $runtimePath 'go/bin/go.exe'
$pgBin = Join-Path $runtimePath 'pgsql/bin'
$temporalPath = Join-Path $runtimePath 'temporal/temporal.exe'
foreach ($path in @($goPath, (Join-Path $pgBin 'pg_ctl.exe'), $temporalPath)) { if (-not (Test-Path -LiteralPath $path)) { throw ('Missing portable runtime: '+$path) } }
Set-Location -LiteralPath $projectRoot
$env:GOPATH = Join-Path $statePath 'gopath'
$env:GOCACHE = Join-Path $statePath 'gocache'
npm ci
if ($LASTEXITCODE -ne 0) { throw 'npm ci failed' }
npm run build
if ($LASTEXITCODE -ne 0) { throw 'Application build failed' }
$apiPath = Join-Path $statePath 'presspilot-api.exe'
Push-Location apps/api
& $goPath build -o $apiPath .
if ($LASTEXITCODE -ne 0) { throw 'Go build failed' }
Pop-Location
$dataPath = Join-Path $statePath 'postgres-data'
if (-not (Test-Path -LiteralPath (Join-Path $dataPath 'PG_VERSION'))) {
 & (Join-Path $pgBin 'initdb.exe') -D $dataPath -U presspilot --auth=trust --encoding=UTF8 --locale=C
 if ($LASTEXITCODE -ne 0) { throw 'PostgreSQL initialization failed' }
}
& (Join-Path $pgBin 'pg_ctl.exe') -D $dataPath status *> $null
if ($LASTEXITCODE -ne 0) {
 & (Join-Path $pgBin 'pg_ctl.exe') -D $dataPath -l (Join-Path $statePath 'postgres.log') -o '-h 127.0.0.1 -p 55432' start
 if ($LASTEXITCODE -ne 0) { throw 'PostgreSQL startup failed; check whether the loopback port is occupied' }
}
$exists = & (Join-Path $pgBin 'psql.exe') -h 127.0.0.1 -p 55432 -U presspilot -d postgres -Atc "SELECT 1 FROM pg_database WHERE datname='presspilot'"
if ($exists -ne '1') { & (Join-Path $pgBin 'createdb.exe') -h 127.0.0.1 -p 55432 -U presspilot presspilot }
$keyPath = Join-Path $statePath 'worker-secret'
if (-not (Test-Path -LiteralPath $keyPath)) { $bytes=[byte[]]::new(32); [Security.Cryptography.RandomNumberGenerator]::Fill($bytes); [IO.File]::WriteAllText($keyPath,[Convert]::ToBase64String($bytes)) }
$env:WORKER_SECRET_FILE=$keyPath
$env:DATABASE_URL='postgres://presspilot@127.0.0.1:55432/presspilot?sslmode=disable'
$env:DATABASE_URL_FILE=''
$env:SANDBOX_ENABLED='true'
$env:TEMPORAL_ADDRESS='127.0.0.1:7233'
$env:TEMPORAL_TLS='false'
$env:BUSINESS_API='http://127.0.0.1:8080'
$env:LIVE_MODEL_ENABLED='false'
$env:WORKER_HEALTH_BIND='127.0.0.1'
$processes=@()
function Start-Tracked([string]$Name,[string]$Program,[string[]]$Arguments) {
 $process=Start-Process -FilePath $Program -ArgumentList $Arguments -WorkingDirectory $projectRoot -WindowStyle Hidden -RedirectStandardOutput (Join-Path $statePath ($Name+'.out.log')) -RedirectStandardError (Join-Path $statePath ($Name+'.err.log')) -PassThru
 $script:processes += @{name=$Name;pid=$process.Id;startTime=$process.StartTime.ToUniversalTime().ToString('o');program=$Program}
}
foreach($port in @(7233,8080,5173)) { if(Get-NetTCPConnection -LocalPort $port -State Listen -ErrorAction SilentlyContinue) { throw ('Port '+$port+' is in use; start-local does not replace existing processes') } }
Start-Tracked 'temporal' $temporalPath @('server','start-dev','--ip','127.0.0.1','--db-filename',('"'+(Join-Path $statePath 'temporal.db')+'"'))
$deadline=(Get-Date).AddSeconds(45)
while(-not(Get-NetTCPConnection -LocalPort 7233 -State Listen -ErrorAction SilentlyContinue)){if((Get-Date)-gt $deadline){throw 'Temporal startup timed out; inspect runtime logs'};Start-Sleep -Milliseconds 300}
Start-Tracked 'api' $apiPath @()
$deadline=(Get-Date).AddSeconds(45)
while($true){try{Invoke-RestMethod 'http://127.0.0.1:8080/healthz' -TimeoutSec 2 > $null;break}catch{if((Get-Date)-gt $deadline){throw 'API startup timed out; inspect runtime logs'};Start-Sleep -Milliseconds 300}}
$nodePath=(Get-Command node.exe).Source
Start-Tracked 'worker' $nodePath @(('"'+(Join-Path $projectRoot 'apps/worker/dist/worker.js')+'"'))
Start-Tracked 'web' $nodePath @(('"'+(Join-Path $projectRoot 'node_modules/vite/bin/vite.js')+'"'),'apps/web','--host','127.0.0.1')
$processes | ConvertTo-Json | Set-Content -LiteralPath (Join-Path $statePath 'processes.json')
Write-Output 'Local PressPilot started at http://localhost:5173. Check runtime logs for readiness. PostgreSQL uses loopback-only trust authentication; Docker uses generated passwords.'
