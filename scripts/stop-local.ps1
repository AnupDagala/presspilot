param([string]$StateRoot='./runtime/native',[string]$RuntimeRoot)
$ErrorActionPreference='Stop'
$statePath=[IO.Path]::GetFullPath($StateRoot)
$records=Get-Content -Raw -LiteralPath (Join-Path $statePath 'processes.json') | ConvertFrom-Json
foreach($record in $records){
 $process=Get-Process -Id $record.pid -ErrorAction SilentlyContinue
 if($process -and $process.StartTime.ToUniversalTime().ToString('o') -eq $record.startTime -and $process.Path -eq $record.program){Stop-Process -Id $process.Id}
}
if($RuntimeRoot){$runtimePath=(Resolve-Path -LiteralPath $RuntimeRoot).Path;& (Join-Path $runtimePath 'pgsql/bin/pg_ctl.exe') -D (Join-Path $statePath 'postgres-data') stop -m fast}
Write-Output 'Tracked processes stopped. Database files and workflow history preserved.'
