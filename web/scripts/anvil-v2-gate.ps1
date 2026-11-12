# Anvil v2 web gate (Plan v2-C Task 9): deploys a local Uniswap V4 stack, the engine v2
# system, Universal Router 2.1.2 and V4Quoter, runs the backend indexer and API against the v2
# manifest, and drives e2e/v2-lifecycle.spec.ts through the browser. Modelled on
# anvil-task6-gate.ps1.
param(
    [int]$AnvilPort = 0,
    [int]$PostgresPort = 0,
    [int]$ApiPort = 0,
    [int]$IndexerHealthPort = 0,
    [int]$WebPort = 0,
    [switch]$KeepAnvil,
    [string]$PlaywrightGrep = ""
)

$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest

$webRoot = Split-Path -Parent $PSScriptRoot
$repoRoot = Split-Path -Parent $webRoot
$backendRoot = Join-Path $repoRoot "backend"
$contractsRoot = Join-Path $repoRoot "contracts"
$manifestPath = Join-Path $contractsRoot "deployments/.generated/v2-anvil.json"
$peripheryPath = [IO.Path]::ChangeExtension($manifestPath, ".v4-periphery.json")
$deploymentId = "v2-anvil"
$sender = "0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266"
$pauseAuthority = "0x70997970C51812dc3A010C7d01b50e0d17dc79C8"
$timelock = "0x3C44CdDdB6a900fa2b585dd299e03d12FA4293BC"
$protocolTreasury = "0x90F79bf6EB2c4f870365E785982E1f101E93b906"
$postgresImage = "postgres:18.6-alpine"
$postgresContainer = "launchpad-v2-postgres-$PID"
$anvilProcess = $null
$apiProcess = $null
$indexerProcess = $null
$postgresStarted = $false
$oldGoCache = $env:GOCACHE
$runningOnWindows = [System.Environment]::OSVersion.Platform -eq [System.PlatformID]::Win32NT
$powershellCommand = if ($runningOnWindows) { "powershell.exe" } else { "pwsh" }
$npmCommand = if ($runningOnWindows) { "npm.cmd" } else { "npm" }
$foundryExtension = if ($runningOnWindows) { ".exe" } else { "" }

function Get-FreeTcpPort {
    $listener = [System.Net.Sockets.TcpListener]::new([System.Net.IPAddress]::Loopback, 0)
    try { $listener.Start(); return ([System.Net.IPEndPoint]$listener.LocalEndpoint).Port }
    finally { $listener.Stop() }
}
function Require-Port([int]$value) {
    if ($value -eq 0) { return Get-FreeTcpPort }
    if ($value -lt 1024 -or $value -gt 65535) { throw "Port must be between 1024 and 65535" }
    return $value
}
function Invoke-Checked([string]$FilePath, [string[]]$Arguments) {
    & $FilePath @Arguments
    if ($LASTEXITCODE -ne 0) { throw "$FilePath failed with exit code $LASTEXITCODE" }
}
function Start-Child([string]$FilePath, [string]$Arguments, [hashtable]$Environment) {
    $label = [IO.Path]::GetFileNameWithoutExtension($FilePath)
    $stdoutLog = Join-Path $backendRoot ".cache/v2-gate-$PID-$label.stdout.log"
    $stderrLog = Join-Path $backendRoot ".cache/v2-gate-$PID-$label.stderr.log"
    New-Item -ItemType File -Path $stdoutLog -Force | Out-Null
    New-Item -ItemType File -Path $stderrLog -Force | Out-Null
    foreach ($entry in $Environment.GetEnumerator()) { Set-Item -Path ("Env:" + $entry.Key) -Value ([string]$entry.Value) }
    if ([string]::IsNullOrWhiteSpace($Arguments)) {
        $child = Start-Process -FilePath $FilePath -WorkingDirectory $backendRoot -PassThru -RedirectStandardOutput $stdoutLog -RedirectStandardError $stderrLog
    } else {
        $child = Start-Process -FilePath $FilePath -ArgumentList $Arguments -WorkingDirectory $backendRoot -PassThru -RedirectStandardOutput $stdoutLog -RedirectStandardError $stderrLog
    }
    if ($null -eq $child) { throw "Could not start $FilePath" }
    return $child
}
function Stop-Child($child) {
    if ($null -eq $child) { return }
    try {
        if (-not $child.HasExited) {
            try { $child.Kill($true) } catch { $child.Kill() }
            $child.WaitForExit(5000) | Out-Null
        }
    } catch { }
    try { $child.Dispose() } catch { }
}
function Wait-Http([string]$Url, [int]$ExpectedStatus = 200, [int]$Attempts = 120) {
    for ($attempt = 0; $attempt -lt $Attempts; $attempt++) {
        try {
            $response = Invoke-WebRequest -UseBasicParsing -Uri $Url -TimeoutSec 2
            if ([int]$response.StatusCode -eq $ExpectedStatus) { return }
        } catch { }
        Start-Sleep -Milliseconds 250
    }
    throw "Timed out waiting for $Url (status $ExpectedStatus)"
}
function Wait-Postgres {
    for ($attempt = 0; $attempt -lt 120; $attempt++) {
        try {
            # The image's temporary init server accepts Unix sockets only. Probe TCP
            # so readiness cannot pass before that server stops and the final server starts.
            # Do not pipe this command: pwsh/Linux can mask docker exec's exit code.
            $readyOutput = & docker exec $postgresContainer pg_isready -h 127.0.0.1 -U postgres -d postgres 2>$null
            $readyExitCode = $LASTEXITCODE
            if ($readyExitCode -eq 0) {
                $probeOutput = & docker exec -e PGPASSWORD=postgres $postgresContainer psql -h 127.0.0.1 -v ON_ERROR_STOP=1 -U postgres -d postgres -c "SELECT 1" 2>$null
                if ($LASTEXITCODE -eq 0) { return }
            }
        } catch { }
        Start-Sleep -Milliseconds 250
    }
    throw "PostgreSQL did not become ready"
}

$anvilPort = Require-Port $AnvilPort
$postgresPort = Require-Port $PostgresPort
$apiPort = Require-Port $ApiPort
$indexerHealthPort = Require-Port $IndexerHealthPort
$webPort = Require-Port $WebPort
$rpcUrl = "http://127.0.0.1:$anvilPort"
$databaseName = "v2gate_$PID"
$databaseUrl = "postgres://postgres:postgres@127.0.0.1:$postgresPort/${databaseName}?sslmode=disable"
$apiUrl = "http://127.0.0.1:$apiPort"
$webUrl = "http://127.0.0.1:$webPort"

foreach ($command in @("docker", "go", $npmCommand, $powershellCommand)) {
    if ($null -eq (Get-Command $command -ErrorAction SilentlyContinue)) { throw "$command is required for the Anvil v2 gate" }
}
$anvilCommand = Get-Command anvil -ErrorAction SilentlyContinue
$forgeCommand = Get-Command forge -ErrorAction SilentlyContinue
$castCommand = Get-Command cast -ErrorAction SilentlyContinue
$userProfile = [Environment]::GetFolderPath([Environment+SpecialFolder]::UserProfile)
$foundryBin = Join-Path $userProfile ".foundry/bin"
$anvilPath = if ($anvilCommand) { $anvilCommand.Path } else { Join-Path $foundryBin "anvil$foundryExtension" }
$forgePath = if ($forgeCommand) { $forgeCommand.Path } else { Join-Path $foundryBin "forge$foundryExtension" }
$castPath = if ($castCommand) { $castCommand.Path } else { Join-Path $foundryBin "cast$foundryExtension" }
foreach ($path in @($anvilPath, $forgePath, $castPath)) {
    if (-not (Test-Path -LiteralPath $path)) { throw "Foundry executable not found: $path" }
}
$env:Path = "$(Split-Path -Parent $anvilPath)$([IO.Path]::PathSeparator)$env:Path"
$env:GOCACHE = Join-Path $backendRoot ".cache/task6-go-build"

try {
    foreach ($generated in @($manifestPath, $peripheryPath)) { if (Test-Path -LiteralPath $generated) { Remove-Item -LiteralPath $generated -Force } }
    Invoke-Checked "docker" @("run", "--name", $postgresContainer, "--detach", "--publish", "$postgresPort`:5432", "--env", "POSTGRES_USER=postgres", "--env", "POSTGRES_PASSWORD=postgres", "--env", "POSTGRES_DB=postgres", $postgresImage)
    $postgresStarted = $true
    Wait-Postgres
    Invoke-Checked "docker" @("exec", $postgresContainer, "psql", "-v", "ON_ERROR_STOP=1", "-U", "postgres", "-d", "postgres", "-c", "CREATE DATABASE $databaseName")

    $anvilStart = [System.Diagnostics.ProcessStartInfo]::new()
    $anvilStart.FileName = $anvilPath
    $anvilStart.Arguments = "--host 127.0.0.1 --port $anvilPort --chain-id 31337 --silent --allow-origin $webUrl"
    $anvilStart.UseShellExecute = $false
    $anvilStart.CreateNoWindow = $true
    $anvilProcess = [System.Diagnostics.Process]::Start($anvilStart)
    for ($attempt = 0; $attempt -lt 120; $attempt++) {
        $chain = (& $castPath chain-id --rpc-url $rpcUrl 2>$null | Out-String).Trim()
        if ($LASTEXITCODE -eq 0 -and $chain -eq "31337") { break }
        if ($anvilProcess.HasExited) { throw "Anvil exited before RPC became ready" }
        Start-Sleep -Milliseconds 100
        if ($attempt -eq 119) { throw "Anvil RPC did not become ready" }
    }

    # The router and V4 periphery come from their own test-only Foundry profiles (D1).
    $previousProfile = [Environment]::GetEnvironmentVariable("FOUNDRY_PROFILE", "Process")
    Push-Location $contractsRoot
    try {
        foreach ($profile in @("v4", "router")) {
            [Environment]::SetEnvironmentVariable("FOUNDRY_PROFILE", $profile, "Process")
            Invoke-Checked $forgePath @("build", "--no-lint")
        }
    } finally {
        [Environment]::SetEnvironmentVariable("FOUNDRY_PROFILE", $previousProfile, "Process")
        Pop-Location
    }
    $deploymentScript = Join-Path $contractsRoot "scripts/deploy-v2-local.ps1"
    Invoke-Checked $powershellCommand @(
        "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", $deploymentScript,
        "-RpcUrl", $rpcUrl, "-DeploymentId", $deploymentId, "-Sender", $sender,
        "-PauseAuthority", $pauseAuthority, "-Timelock", $timelock,
        "-ProtocolTreasury", $protocolTreasury, "-OutputPath", $manifestPath, "-WithRouter"
    )
    if (-not (Test-Path -LiteralPath $manifestPath)) { throw "Local v2 deployment did not produce a manifest" }
    if (-not (Test-Path -LiteralPath $peripheryPath)) { throw "Local v2 deployment did not produce the V4 periphery record" }
    $manifest = Get-Content -Raw -LiteralPath $manifestPath | ConvertFrom-Json
    $periphery = Get-Content -Raw -LiteralPath $peripheryPath | ConvertFrom-Json
    $v2Addresses = [ordered]@{
        FACTORY = [string]$manifest.factory
        POOL_MANAGER = [string]$manifest.poolManager
        PERMIT2 = [string]$manifest.permit2
        HOOK = [string]$manifest.hook
        LOCKER = [string]$manifest.locker
        UNIVERSAL_ROUTER = [string]$periphery.universalRouter
        V4_QUOTER = [string]$periphery.v4Quoter
    }
    foreach ($entry in $v2Addresses.GetEnumerator()) {
        if ($entry.Value -notmatch "^0x[0-9a-fA-F]{40}$") { throw "Invalid generated $($entry.Key) address" }
    }

    $oldDatabaseUrl = $env:DATABASE_URL
    $env:DATABASE_URL = $databaseUrl
    Push-Location $backendRoot
    try {
        try { Invoke-Checked "go" @("run", "./cmd/migrate", "up") } finally {
            if ($null -eq $oldDatabaseUrl) { Remove-Item Env:DATABASE_URL -ErrorAction SilentlyContinue } else { $env:DATABASE_URL = $oldDatabaseUrl }
        }
    } finally {
        Pop-Location
    }

    $runtimeEnv = @{
        CHAIN_ID = "31337"; DEPLOYMENT_ID = $deploymentId; RPC_URL = $rpcUrl; DATABASE_URL = $databaseUrl
        API_ADDR = "127.0.0.1:$apiPort"; API_ALLOWED_ORIGINS = $webUrl; INDEXER_HEALTH_ADDR = "127.0.0.1:$indexerHealthPort"
        INDEXER_CHUNK_SIZE = "100"; INDEXER_LOG_ADDRESS_BATCH_SIZE = "500"; INDEXER_POLL_INTERVAL = "200ms"
        RPC_TIMEOUT = "2s"; RPC_MAX_RETRIES = "2"; RPC_RETRY_BACKOFF = "50ms"; INDEXER_WORKER_ID = "v2-gate-indexer"
        INDEXER_CONFIRMATIONS = "0"; PRIVY_APP_ID = "v2-anvil"
        PRIVY_VERIFICATION_KEY = "-----BEGIN PUBLIC KEY-----`nMFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAEtOOEaFeGAEoHOSkD0owE7VmvwjcI`n3lpjzKdfLSJaQvLUZnKvBdO3mWLsvulsCuB0PJCgycRbVvqZqyhN1txH4A==`n-----END PUBLIC KEY-----`n"
        DEPLOYMENT_MANIFEST_PATH = $manifestPath
    }
    $apiBinary = Join-Path $backendRoot ".cache/v2-gate-api.exe"
    $indexerBinary = Join-Path $backendRoot ".cache/v2-gate-indexer.exe"
    Push-Location $backendRoot
    try {
        Invoke-Checked "go" @("build", "-o", $apiBinary, "./cmd/api")
        Invoke-Checked "go" @("build", "-o", $indexerBinary, "./cmd/indexer")
    } finally {
        Pop-Location
    }
    $apiProcess = Start-Child $apiBinary "" $runtimeEnv
    Wait-Http "$apiUrl/healthz"
    $indexerProcess = Start-Child $indexerBinary "" $runtimeEnv
    Start-Sleep -Milliseconds 250
    if ($indexerProcess.HasExited) { throw "Indexer exited immediately with code $($indexerProcess.ExitCode)" }
    Wait-Http "http://127.0.0.1:$indexerHealthPort/healthz"
    if ($indexerProcess.HasExited) { throw "Indexer exited during readiness with code $($indexerProcess.ExitCode)" }

    $env:V2_ANVIL_REQUIRED = "1"
    $env:TASK6_ANVIL_RPC_URL = $rpcUrl
    $env:TASK6_ANVIL_API_URL = $apiUrl
    $env:TASK6_WEB_PORT = [string]$webPort
    foreach ($entry in $v2Addresses.GetEnumerator()) { Set-Item -Path ("Env:V2_ANVIL_" + $entry.Key) -Value $entry.Value }
    Push-Location $webRoot
    try {
        # One chain, one lifecycle: the spec mutates shared Anvil state (time, graduation), so
        # it runs on a single project.
        $playwrightArgs = @("run", "test:e2e", "--", "e2e/v2-lifecycle.spec.ts", "--project=small-laptop")
        if (-not [string]::IsNullOrWhiteSpace($PlaywrightGrep)) { $playwrightArgs += @("-g", $PlaywrightGrep) }
        Invoke-Checked $npmCommand $playwrightArgs
    }
    finally { Pop-Location }
}
finally {
    Remove-Item Env:V2_ANVIL_REQUIRED, Env:TASK6_ANVIL_RPC_URL, Env:TASK6_ANVIL_API_URL, Env:TASK6_WEB_PORT -ErrorAction SilentlyContinue
    foreach ($name in @("FACTORY", "POOL_MANAGER", "PERMIT2", "HOOK", "LOCKER", "UNIVERSAL_ROUTER", "V4_QUOTER")) { Remove-Item "Env:V2_ANVIL_$name" -ErrorAction SilentlyContinue }
    Stop-Child $indexerProcess
    Stop-Child $apiProcess
    if ($postgresStarted) {
        try { & docker exec $postgresContainer psql -v ON_ERROR_STOP=1 -U postgres -d postgres -c "DROP DATABASE IF EXISTS $databaseName WITH (FORCE)" | Out-Null } catch { }
        try { & docker rm --force $postgresContainer | Out-Null } catch { }
    }
    foreach ($generated in @($manifestPath, $peripheryPath)) { if (Test-Path -LiteralPath $generated) { Remove-Item -LiteralPath $generated -Force } }
    if ($null -ne $anvilProcess -and -not $KeepAnvil) { Stop-Child $anvilProcess }
    if ($null -eq $oldGoCache) { Remove-Item Env:GOCACHE -ErrorAction SilentlyContinue } else { $env:GOCACHE = $oldGoCache }
}
