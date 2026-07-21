[CmdletBinding()]
param(
    [string]$BaseUrl = "http://127.0.0.1:8081",
    [string]$ConfigPath = ".\config_same4_v3_good.txt",
    [string]$RunId = ("go-native-{0}" -f (Get-Date -Format "yyyyMMdd-HHmmss")),
    [string]$RequestedBy = $env:USERNAME,
    [ValidateRange(1, 86400)]
    [int]$DurationSeconds = 10,
    [switch]$PeriodicTestPulse
)

$ErrorActionPreference = "Stop"

function Invoke-DaqRequest {
    param(
        [Parameter(Mandatory = $true)][string]$Service,
        [Parameter(Mandatory = $true)][string]$Method,
        [Parameter(Mandatory = $true)][hashtable]$Message
    )

    $json = ConvertTo-Json -InputObject $Message -Depth 10 -Compress
    $bytes = [System.Text.Encoding]::UTF8.GetBytes($json)
    Invoke-RestMethod `
        -Method Post `
        -Uri "$BaseUrl/pet.caen.daq.v1.$Service/$Method" `
        -ContentType "application/json; charset=utf-8" `
        -Body $bytes
}

function Get-DaqSnapshot {
    Invoke-DaqRequest -Service "SystemService" -Method "GetSystemSnapshot" -Message @{}
}

$resolvedConfig = Resolve-Path -LiteralPath $ConfigPath
[string]$configuration = [System.IO.File]::ReadAllText($resolvedConfig)

if ($PeriodicTestPulse) {
	$pattern = '(?m)^(\s*TestPulseSource\s+)(OFF|PTRG)(\s*(?:#.*)?)$'
    $matches = [regex]::Matches($configuration, $pattern)
    if ($matches.Count -ne 1) {
		throw "Expected exactly one TestPulseSource setting with value OFF or PTRG, found $($matches.Count)."
    }
	$configuration = [regex]::Replace($configuration, $pattern, '${1}PTRG${3}')
    Write-Host "Periodic test pulses enabled in submitted configuration only."
}

$initial = Get-DaqSnapshot
if ($initial.state -ne "SYSTEM_STATE_READY") {
    throw "DAQ is not ready; current state is $($initial.state). Restart a faulted backend before running this script."
}

Write-Host "Starting run $RunId for $DurationSeconds seconds..."
$start = Invoke-DaqRequest -Service "RunService" -Method "StartRun" -Message @{
    runId              = $RunId
    requestedBy        = $RequestedBy
    janusConfiguration = $configuration
    captureRaw         = $true
    journalTransport   = $true
}

$started = $true
try {
    if ($start.snapshot.state -ne "SYSTEM_STATE_RUNNING") {
        throw "StartRun returned state $($start.snapshot.state), expected SYSTEM_STATE_RUNNING."
    }

    for ($elapsed = 1; $elapsed -le $DurationSeconds; $elapsed++) {
        Start-Sleep -Seconds 1
        $status = Get-DaqSnapshot
        $decoded = $status.snapshot.pipeline.decodedEvents
        if ($null -eq $decoded) { $decoded = 0 }
        Write-Host ("[{0,3}/{1}] state={2} decoded_events={3}" -f $elapsed, $DurationSeconds, $status.state, $decoded)
        if ($status.state -ne "SYSTEM_STATE_RUNNING") {
            $faults = @($status.snapshot.diagnostics | Where-Object { $_.severity -eq "DIAGNOSTIC_SEVERITY_ERROR" })
            foreach ($fault in $faults) {
                Write-Error ("{0}: {1}" -f $fault.code, $fault.message)
            }
            throw "Run left SYSTEM_STATE_RUNNING before the requested duration completed."
        }
    }

    Write-Host "Stopping and draining run $RunId..."
    $stop = Invoke-DaqRequest -Service "RunService" -Method "StopRun" -Message @{
        runId       = $RunId
        requestedBy = $RequestedBy
    }
	$started = $false

	$eventCount = $stop.run.eventCount
	if ($null -eq $eventCount) { $eventCount = 0 }
	$rawBatchCount = $stop.run.rawBatchCount
	if ($null -eq $rawBatchCount) { $rawBatchCount = 0 }
	$incomplete = $stop.run.incomplete
	if ($null -eq $incomplete) { $incomplete = $false }
    Write-Host ("Completed run={0} events={1} raw_batches={2} incomplete={3}" -f `
		$stop.run.runId, $eventCount, $rawBatchCount, $incomplete)
    if ($stop.run.artifacts) {
        $stop.run.artifacts | Format-Table kind, name, sizeBytes, sha256 -AutoSize
    }
    $stop | ConvertTo-Json -Depth 20
}
finally {
    if ($started) {
        try {
            $current = Get-DaqSnapshot
            if ($current.state -eq "SYSTEM_STATE_RUNNING" -and $current.snapshot.currentRun.runId -eq $RunId) {
                Write-Warning "Attempting orderly stop after script failure or interruption."
                Invoke-DaqRequest -Service "RunService" -Method "StopRun" -Message @{
                    runId       = $RunId
                    requestedBy = $RequestedBy
                } | Out-Null
            }
        }
        catch {
            Write-Warning "Automatic stop failed: $($_.Exception.Message)"
        }
    }
}
