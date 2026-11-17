$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest

# Deploys the local Uniswap V4 stack and the engine v2 system on a fresh Anvil chain, then runs
# the Go v2 lifecycle and reorg acceptance test against it (Plan v2-B Task 11).

$backendRoot = Split-Path -Parent $PSScriptRoot
$repositoryRoot = Split-Path -Parent $backendRoot
$contractsRoot = Join-Path $repositoryRoot "contracts"
$deploymentId = "anvil-v2-indexer-e2e"
$manifestPath = Join-Path $contractsRoot "deployments/.generated/$deploymentId.json"
$broadcastPath = Join-Path $contractsRoot "broadcast/DeployLocalV2.s.sol/31337/run-latest.json"
# Anvil's first four dev accounts; the test drives the lifecycle from the next three.
$sender = "0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266"
$pauseAuthority = "0x70997970C51812dc3A010C7d01b50e0d17dc79C8"
$timelock = "0x3C44CdDdB6a900fa2b585dd299e03d12FA4293BC"
$protocolTreasury = "0x90F79bf6EB2c4f870365E785982E1f101E93b906"
$anvilProcess = $null
$environmentKeys = @(
    "ANVIL_V2_RPC_URL", "ANVIL_V2_FACTORY", "ANVIL_V2_START_BLOCK", "ANVIL_V2_POOL_MANAGER",
    "ANVIL_V2_LOCKER", "ANVIL_V2_HOOK", "ANVIL_V2_SWAP_ROUTER", "ANVIL_V2_TREASURY"
)

function Fail([string] $Message) { throw "Anvil v2 indexer E2E failed: $Message" }

function Get-FreeTcpPort {
    $listener = [System.Net.Sockets.TcpListener]::new([System.Net.IPAddress]::Loopback, 0)
    try { $listener.Start(); return ([System.Net.IPEndPoint] $listener.LocalEndpoint).Port }
    finally { $listener.Stop() }
}

foreach ($command in @("anvil", "cast", "forge")) {
    if ($null -eq (Get-Command $command -ErrorAction SilentlyContinue)) { Fail "$command is not on PATH" }
}

# The local deployment loads the test-only Uniswap v4 artifacts from out-v4/.
Push-Location $contractsRoot
$previousProfile = [Environment]::GetEnvironmentVariable("FOUNDRY_PROFILE", "Process")
try {
    [Environment]::SetEnvironmentVariable("FOUNDRY_PROFILE", "v4", "Process")
    & forge build --no-lint | Out-Host
    if ($LASTEXITCODE -ne 0) { Fail "forge build of the v4 artifacts failed" }
}
finally {
    [Environment]::SetEnvironmentVariable("FOUNDRY_PROFILE", $previousProfile, "Process")
    Pop-Location
}

$port = Get-FreeTcpPort
$rpcURL = "http://127.0.0.1:$port"
$start = [System.Diagnostics.ProcessStartInfo]::new()
$start.FileName = (Get-Command anvil).Source
# Cancun matches the contracts' evm_version; the code-size limit is lifted for the via-IR
# PositionManager build, as in the contract tests.
$start.Arguments = "--host 127.0.0.1 --port $port --chain-id 31337 --hardfork cancun --code-size-limit 50000 --silent"
$start.UseShellExecute = $false
$start.CreateNoWindow = $true

try {
    if (Test-Path -LiteralPath $manifestPath) { Remove-Item -LiteralPath $manifestPath -Force }
    $anvilProcess = [System.Diagnostics.Process]::Start($start)
    if ($null -eq $anvilProcess) { Fail "could not start Anvil" }
    for ($attempt = 0; $attempt -lt 100; ++$attempt) {
        $chainID = (& cast chain-id --rpc-url $rpcURL 2>$null | Out-String).Trim()
        if ($LASTEXITCODE -eq 0 -and $chainID -eq "31337") { break }
        if ($anvilProcess.HasExited) { Fail "Anvil exited before becoming ready" }
        Start-Sleep -Milliseconds 100
        if ($attempt -eq 99) { Fail "Anvil RPC did not become ready" }
    }

    & "$contractsRoot/scripts/deploy-v2-local.ps1" -RpcUrl $rpcURL -DeploymentId $deploymentId -Sender $sender -PauseAuthority $pauseAuthority -Timelock $timelock -ProtocolTreasury $protocolTreasury -OutputPath $manifestPath
    if (-not (Test-Path -LiteralPath $manifestPath)) { Fail "deployment did not produce a manifest" }
    $manifest = Get-Content -Raw -LiteralPath $manifestPath | ConvertFrom-Json
    # PoolSwapTest is a test-only router, so it is not in the manifest; read it from the broadcast.
    $record = Get-Content -Raw -LiteralPath $broadcastPath | ConvertFrom-Json
    $routers = @($record.transactions | Where-Object { $_.contractName -eq "PoolSwapTest" -and $_.transactionType -eq "CREATE" })
    if ($routers.Count -ne 1) { Fail "expected one PoolSwapTest creation, found $($routers.Count)" }

    $env:ANVIL_V2_RPC_URL = $rpcURL
    $env:ANVIL_V2_FACTORY = [string]$manifest.factory
    $env:ANVIL_V2_START_BLOCK = [string]$manifest.startBlock
    $env:ANVIL_V2_POOL_MANAGER = [string]$manifest.poolManager
    $env:ANVIL_V2_LOCKER = [string]$manifest.locker
    $env:ANVIL_V2_HOOK = [string]$manifest.hook
    $env:ANVIL_V2_SWAP_ROUTER = [string]$routers[0].contractAddress
    $env:ANVIL_V2_TREASURY = $protocolTreasury
    Push-Location $backendRoot
    try {
        go test -tags=integration -race -count=1 -run '^(TestAnvilV2LifecycleAndReorg|TestAnvilV2DeepReorgSoak)$' ./internal/indexer/...
        if ($LASTEXITCODE -ne 0) { Fail "Go v2 indexer test failed" }
    }
    finally { Pop-Location }
}
finally {
    foreach ($key in $environmentKeys) { Remove-Item "Env:$key" -ErrorAction SilentlyContinue }
    if (Test-Path -LiteralPath $manifestPath) { Remove-Item -LiteralPath $manifestPath -Force }
    if ($null -ne $anvilProcess) {
        if (-not $anvilProcess.HasExited) { $anvilProcess.Kill(); $anvilProcess.WaitForExit() }
        $anvilProcess.Dispose()
    }
}
