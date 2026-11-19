param(
    [Parameter(Mandatory = $true)]
    [string] $RpcUrl,

    [Parameter(Mandatory = $true)]
    [ValidatePattern("^[a-z0-9][a-z0-9._-]{2,63}$")]
    [string] $DeploymentId,

    [Parameter(Mandatory = $true)]
    [ValidatePattern("^0x[0-9a-fA-F]{40}$")]
    [string] $Sender,

    [Parameter(Mandatory = $true)]
    [ValidatePattern("^0x[0-9a-fA-F]{40}$")]
    [string] $PauseAuthority,

    [Parameter(Mandatory = $true)]
    [ValidatePattern("^0x[0-9a-fA-F]{40}$")]
    [string] $Timelock,

    [Parameter(Mandatory = $true)]
    [ValidatePattern("^0x[0-9a-fA-F]{40}$")]
    [string] $ProtocolTreasury,

    [switch] $Broadcast,
    [switch] $Unlocked,
    [string] $Account
)

# Deploys engine 2 on Robinhood testnet (46630) through script/DeployLaunchpadV2.s.sol against the
# reviewed self-deployed v4 record, then writes an unreviewed candidate manifest to
# deployments/.generated/<DeploymentId>.json. Decisions:
# docs/specs/2026-11-18-v2-testnet-path-preflight.md. Without -Broadcast it only simulates.

$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest

$contractsRoot = Split-Path -Parent $PSScriptRoot
$deploymentsRoot = Join-Path $contractsRoot "deployments"
$v4RecordPath = Join-Path $deploymentsRoot "config/robinhood-testnet-v4.json"
$utf8WithoutBom = New-Object System.Text.UTF8Encoding($false)
$chainId = 46630
$engineVersion = 2

function Fail([string] $Message) {
    throw "Testnet v2 deployment failed: $Message"
}

function Invoke-Cast([string[]] $Arguments) {
    $value = (& cast @Arguments --rpc-url $RpcUrl | Out-String).Trim()
    if ($LASTEXITCODE -ne 0) { Fail "cast $($Arguments -join ' ') exited with $LASTEXITCODE" }
    return $value
}

function Get-Address([string] $Target, [string] $Signature, [string[]] $Arguments = @()) {
    $value = Invoke-Cast (@("call", $Target, $Signature) + $Arguments)
    if ($value -notmatch "^0x[0-9a-fA-F]{40}$" -or $value -eq ("0x" + ("0" * 40))) {
        Fail "$Signature on $Target returned $value"
    }
    return $value
}

function Get-CodeHash([string] $Address) {
    $value = Invoke-Cast @("codehash", $Address)
    if ($value -notmatch "^0x[0-9a-f]{64}$" -or $value -eq ("0x" + ("0" * 64))) {
        Fail "runtime code is absent at $Address"
    }
    return $value
}

function Get-Creation([object] $Record, [string] $ContractName) {
    $found = @(
        $Record.transactions | Where-Object {
            $_.contractName -eq $ContractName -and $_.transactionType -eq "CREATE"
        }
    )
    if ($found.Count -ne 1) {
        Fail "expected exactly one $ContractName creation transaction, found $($found.Count)"
    }
    return $found[0]
}

function Get-CreationReceipt([object] $Record, [string] $ContractAddress) {
    $found = @(
        $Record.receipts | Where-Object { [string] $_.contractAddress -ieq $ContractAddress }
    )
    if ($found.Count -ne 1) {
        Fail "expected exactly one creation receipt for $ContractAddress, found $($found.Count)"
    }
    return $found[0]
}

function Convert-HexNumber([string] $Value) {
    if ($Value -notmatch "^0x[0-9a-fA-F]+$") { Fail "expected a hexadecimal quantity, got $Value" }
    return [Convert]::ToUInt64($Value.Substring(2), 16)
}

if ($Unlocked -and -not [string]::IsNullOrWhiteSpace($Account)) {
    Fail "choose either -Unlocked or -Account, not both"
}
if ($Broadcast -and -not $Unlocked -and [string]::IsNullOrWhiteSpace($Account)) {
    Fail "broadcast requires -Unlocked or a named Foundry -Account"
}
if (-not (Test-Path -LiteralPath $v4RecordPath)) {
    Fail "no reviewed testnet v4 record at $v4RecordPath; run bootstrap-testnet-v4.ps1 and review it first"
}
$v4Record = Get-Content -Raw -LiteralPath $v4RecordPath | ConvertFrom-Json
if ($v4Record.reviewed -ne $true -or [uint64] $v4Record.chainId -ne $chainId) {
    Fail "the testnet v4 record is not a reviewed chain $chainId record"
}

$observedChainId = Invoke-Cast @("chain-id")
if ($observedChainId -ne [string] $chainId) {
    Fail "expected Robinhood testnet chain id $chainId, got $observedChainId"
}

$deploymentEnvironment = [ordered]@{
    DEPLOYER = $Sender
    PAUSE_AUTHORITY = $PauseAuthority
    TIMELOCK = $Timelock
    PROTOCOL_TREASURY = $ProtocolTreasury
}
$previousEnvironment = @{}
Push-Location $contractsRoot
try {
    foreach ($entry in $deploymentEnvironment.GetEnumerator()) {
        $previousEnvironment[$entry.Key] = [Environment]::GetEnvironmentVariable($entry.Key, "Process")
        [Environment]::SetEnvironmentVariable($entry.Key, [string] $entry.Value, "Process")
    }

    $arguments = @(
        "script", "script/DeployLaunchpadV2.s.sol:DeployLaunchpadV2",
        "--rpc-url", $RpcUrl, "--sender", $Sender
    )
    if ($Broadcast) {
        $arguments += "--broadcast"
        if ($Unlocked) { $arguments += "--unlocked" } else { $arguments += @("--account", $Account) }
    }
    & forge @arguments
    if ($LASTEXITCODE -ne 0) { Fail "forge script DeployLaunchpadV2 exited with $LASTEXITCODE" }

    if (-not $Broadcast) {
        Write-Output "Testnet v2 deployment simulation passed. Nothing was broadcast."
        return
    }

    $broadcastPath = Join-Path $contractsRoot "broadcast/DeployLaunchpadV2.s.sol/$chainId/run-latest.json"
    if (-not (Test-Path -LiteralPath $broadcastPath)) { Fail "missing Foundry broadcast record $broadcastPath" }
    $record = Get-Content -Raw -LiteralPath $broadcastPath | ConvertFrom-Json
    $factory = [string] (Get-Creation $record "LaunchFactoryV2").contractAddress
    $factoryReceipt = Get-CreationReceipt $record $factory

    # Every other address is read back from the factory and checked against the reviewed record.
    $poolManager = Get-Address $factory "POOL_MANAGER()(address)"
    $positionManager = Get-Address $factory "POSITION_MANAGER()(address)"
    $permit2 = Get-Address $factory "PERMIT2()(address)"
    $hook = Get-Address $factory "hook()(address)"
    $locker = Get-Address $factory "locker()(address)"
    $curveImplementation = Get-Address $factory "curveImplementation(uint16)(address)" @([string] $engineVersion)
    foreach ($pair in @(
            @("poolManager", $poolManager), @("positionManager", $positionManager), @("permit2", $permit2)
        )) {
        if ($pair[1] -ine [string] $v4Record.($pair[0])) {
            Fail "factory $($pair[0]) $($pair[1]) differs from the reviewed testnet v4 record"
        }
    }
    $hookFlags = [Convert]::ToUInt32($hook.Substring($hook.Length - 4), 16) -band 0x3FFF
    if ($hookFlags -ne 0x2000) { Fail "hook $hook does not carry exactly the BEFORE_INITIALIZE flag" }
    foreach ($dependent in @($hook, $locker)) {
        if ((Get-Address $dependent "FACTORY()(address)") -ine $factory) {
            Fail "$dependent is not wired to factory $factory"
        }
    }
    foreach ($authority in @(
            @("pauseAuthority()(address)", $PauseAuthority),
            @("timelock()(address)", $Timelock),
            @("protocolTreasury()(address)", $ProtocolTreasury)
        )) {
        if ((Get-Address $factory $authority[0]) -ine $authority[1]) {
            Fail "factory $($authority[0]) is not $($authority[1])"
        }
    }

    $forgeVersionOutput = (& forge --version | Out-String)
    if ($forgeVersionOutput -notmatch "Version:\s+([0-9]+\.[0-9]+\.[0-9]+)") {
        Fail "could not determine Foundry version"
    }
    # Captured now: every later -match (for example in Get-CodeHash) overwrites $Matches.
    $foundryVersion = $Matches[1]

    $manifest = [ordered]@{
        schemaVersion = 1
        deploymentId = $DeploymentId
        name = "Robinhood Chain Testnet"
        environment = "testnet"
        chainId = $chainId
        factory = $factory
        startBlock = Convert-HexNumber ([string] $factoryReceipt.blockNumber)
        engineVersion = $engineVersion
        curveImplementation = $curveImplementation
        poolManager = $poolManager
        positionManager = $positionManager
        permit2 = $permit2
        hook = $hook
        locker = $locker
        pool = [ordered]@{
            currency0 = "0x0000000000000000000000000000000000000000"
            fee = 10000
            tickSpacing = 200
        }
        explorerBase = "https://explorer.testnet.chain.robinhood.com"
        graduationEnabled = $true
        deployTransaction = [string] $factoryReceipt.transactionHash
        bytecodeHashes = [ordered]@{
            launchFactory = Get-CodeHash $factory
            bondingCurveV2 = Get-CodeHash $curveImplementation
            hook = Get-CodeHash $hook
            locker = Get-CodeHash $locker
            poolManager = Get-CodeHash $poolManager
            positionManager = Get-CodeHash $positionManager
            permit2 = Get-CodeHash $permit2
        }
        compiler = [ordered]@{
            solcVersion = "0.8.36"
            optimizer = $true
            optimizerRuns = 200
            viaIr = $false
            evmVersion = "cancun"
        }
        toolchain = [ordered]@{ foundryVersion = $foundryVersion }
        governance = [ordered]@{
            pauseAuthority = $PauseAuthority
            timelock = $Timelock
            protocolTreasury = $ProtocolTreasury
            deployer = $Sender
        }
        verification = [ordered]@{
            dependenciesReviewed = $true
            hookAddressVerified = $true
            dependenciesWired = $true
            noResidualDeployerAuthority = $true
        }
    }

    $json = ($manifest | ConvertTo-Json -Depth 20) + "`n"
    $schemaPath = Join-Path $deploymentsRoot "deployment-v2.schema.json"
    if (Get-Command Test-Json -ErrorAction SilentlyContinue) {
        if (-not (Test-Json -Json $json -SchemaFile $schemaPath -ErrorAction Stop)) {
            Fail "candidate manifest failed deployment-v2.schema.json"
        }
    }
    $outputPath = Join-Path $deploymentsRoot ".generated/$DeploymentId.json"
    [System.IO.Directory]::CreateDirectory((Split-Path -Parent $outputPath)) | Out-Null
    [System.IO.File]::WriteAllText($outputPath, $json, $utf8WithoutBom)
    Write-Output "Generated unreviewed testnet engine-2 candidate manifest $outputPath"
}
finally {
    foreach ($entry in $deploymentEnvironment.GetEnumerator()) {
        if ($previousEnvironment.ContainsKey($entry.Key)) {
            [Environment]::SetEnvironmentVariable($entry.Key, $previousEnvironment[$entry.Key], "Process")
        }
    }
    Pop-Location
}
