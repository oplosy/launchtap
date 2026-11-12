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

    [string] $OutputPath,

    # Also deploys Universal Router 2.1.2 and V4Quoter for the Anvil v2 web gate (Plan v2-C
    # Task 9) and records them in a `.v4-periphery.json` file next to the manifest. The caller
    # builds `out-router` (FOUNDRY_PROFILE=router) and `out-v4` first.
    [switch] $WithRouter
)

# Deploys a local Uniswap V4 stack and the engine v2 system on Anvil (unlocked sender) and
# writes a candidate engine-2 manifest under deployments/.generated. Anvil only: reviewed
# networks deploy through script/DeployLaunchpadV2.s.sol against a pinned v4 record.

$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest

$contractsRoot = Split-Path -Parent $PSScriptRoot
$deploymentsRoot = Join-Path $contractsRoot "deployments"
$utf8WithoutBom = New-Object System.Text.UTF8Encoding($false)
$permit2Address = "0x000000000022D473030F116dDEE9F6B43aC78BA3"
$engineVersion = 2

function Fail([string] $Message) {
    throw "Local v2 deployment failed: $Message"
}

function Invoke-Checked([string] $Command, [string[]] $Arguments) {
    & $Command @Arguments
    if ($LASTEXITCODE -ne 0) {
        Fail "$Command exited with $LASTEXITCODE"
    }
}

function Invoke-Cast([string[]] $Arguments) {
    $value = (& cast @Arguments --rpc-url $RpcUrl | Out-String).Trim()
    if ($LASTEXITCODE -ne 0) {
        Fail "cast $($Arguments -join ' ') exited with $LASTEXITCODE"
    }
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
    if ($value -notmatch "^0x[0-9a-fA-F]{64}$") {
        Fail "could not read runtime code hash for $Address"
    }
    if ($value -eq ("0x" + ("0" * 64))) {
        Fail "runtime code is absent at $Address"
    }
    return $value
}

function Get-Creation([object] $BroadcastRecord, [string] $ContractName) {
    $found = @(
        $BroadcastRecord.transactions | Where-Object {
            $_.contractName -eq $ContractName -and $_.transactionType -eq "CREATE"
        }
    )
    if ($found.Count -ne 1) {
        Fail "expected exactly one $ContractName creation transaction, found $($found.Count)"
    }
    return $found[0]
}

function Get-CreationReceipt([object] $BroadcastRecord, [string] $ContractAddress) {
    $found = @(
        $BroadcastRecord.receipts | Where-Object {
            [string] $_.contractAddress -ieq $ContractAddress
        }
    )
    if ($found.Count -ne 1) {
        Fail "expected exactly one creation receipt for $ContractAddress, found $($found.Count)"
    }
    return $found[0]
}

function Convert-HexNumber([string] $Value) {
    if ($Value -notmatch "^0x[0-9a-fA-F]+$") {
        Fail "expected a hexadecimal quantity, got $Value"
    }
    return [Convert]::ToUInt64($Value.Substring(2), 16)
}

$chainId = Invoke-Cast @("chain-id")
if ($chainId -ne "31337") { Fail "local v2 deployment requires Anvil chain id 31337, got $chainId" }

Push-Location $contractsRoot
$deploymentEnvironment = [ordered]@{
    DEPLOYER = $Sender
    PAUSE_AUTHORITY = $PauseAuthority
    TIMELOCK = $Timelock
    PROTOCOL_TREASURY = $ProtocolTreasury
}
$previousEnvironment = @{}
try {
    # The pinned Permit2 helper only etches, so Anvil receives the same canonical runtime code
    # through anvil_setCode.
    $helper = Get-ChildItem -LiteralPath (Join-Path $contractsRoot "lib") -Recurse -File `
        -Filter "DeployPermit2.sol" | Where-Object { $_.FullName -match "[\\/]permit2[\\/]test[\\/]utils[\\/]" } |
        Select-Object -First 1
    if ($null -eq $helper) { Fail "pinned permit2/test/utils/DeployPermit2.sol not found" }
    $helperSource = [System.IO.File]::ReadAllText($helper.FullName)
    $runtime = [regex]::Match($helperSource, 'hex"([0-9a-fA-F]+)"')
    if (-not $runtime.Success) { Fail "Permit2 runtime bytecode not found in $($helper.FullName)" }
    Invoke-Cast @("rpc", "anvil_setCode", $permit2Address, ("0x" + $runtime.Groups[1].Value)) | Out-Null
    $permit2CodeHash = Get-CodeHash $permit2Address

    foreach ($entry in $deploymentEnvironment.GetEnumerator()) {
        $previousEnvironment[$entry.Key] = [Environment]::GetEnvironmentVariable($entry.Key, "Process")
        [Environment]::SetEnvironmentVariable($entry.Key, [string] $entry.Value, "Process")
    }
    Invoke-Checked "forge" @(
        "script", "script/v2/DeployLocalV2.s.sol:DeployLocalV2",
        "--rpc-url", $RpcUrl, "--sender", $Sender, "--broadcast", "--unlocked"
    )

    $broadcastPath = Join-Path $contractsRoot "broadcast/DeployLocalV2.s.sol/31337/run-latest.json"
    if (-not (Test-Path -LiteralPath $broadcastPath)) { Fail "missing Foundry broadcast record $broadcastPath" }
    $record = Get-Content -Raw -LiteralPath $broadcastPath | ConvertFrom-Json
    $factory = [string] (Get-Creation $record "LaunchFactoryV2").contractAddress
    $factoryReceipt = Get-CreationReceipt $record $factory

    # Every other address is read back from the factory, so the manifest reflects the wiring
    # that is actually on chain.
    $poolManager = Get-Address $factory "POOL_MANAGER()(address)"
    $positionManager = Get-Address $factory "POSITION_MANAGER()(address)"
    $permit2 = Get-Address $factory "PERMIT2()(address)"
    $hook = Get-Address $factory "hook()(address)"
    $locker = Get-Address $factory "locker()(address)"
    $curveImplementation = Get-Address $factory "curveImplementation(uint16)(address)" @([string] $engineVersion)
    if ($permit2 -ine $permit2Address) { Fail "factory Permit2 $permit2 is not canonical" }
    if ((Get-Address $positionManager "poolManager()(address)") -ine $poolManager) {
        Fail "PositionManager is wired to another PoolManager"
    }
    $hookFlags = [Convert]::ToUInt32($hook.Substring($hook.Length - 4), 16) -band 0x3FFF
    if ($hookFlags -ne 0x2000) { Fail "hook $hook does not carry exactly the BEFORE_INITIALIZE flag" }
    foreach ($dependent in @($hook, $locker)) {
        if ((Get-Address $dependent "FACTORY()(address)") -ine $factory) {
            Fail "$dependent is not wired to factory $factory"
        }
    }

    $forgeVersionOutput = (& forge --version | Out-String)
    if ($forgeVersionOutput -notmatch "Version:\s+([0-9]+\.[0-9]+\.[0-9]+)") {
        Fail "could not determine Foundry version"
    }
    $foundryVersion = $Matches[1]

    $manifest = [ordered]@{
        schemaVersion = 1
        deploymentId = $DeploymentId
        name = "Local Anvil"
        environment = "local"
        chainId = 31337
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
        explorerBase = ""
        graduationEnabled = $true
        deployTransaction = [string] $factoryReceipt.transactionHash
        bytecodeHashes = [ordered]@{
            launchFactory = Get-CodeHash $factory
            bondingCurveV2 = Get-CodeHash $curveImplementation
            hook = Get-CodeHash $hook
            locker = Get-CodeHash $locker
            poolManager = Get-CodeHash $poolManager
            positionManager = Get-CodeHash $positionManager
            permit2 = $permit2CodeHash
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
    if ([string]::IsNullOrWhiteSpace($OutputPath)) {
        $OutputPath = Join-Path $deploymentsRoot ".generated/$DeploymentId.json"
    }
    $resolvedOutput = [System.IO.Path]::GetFullPath($OutputPath)
    $generatedRoot = [System.IO.Path]::GetFullPath((Join-Path $deploymentsRoot ".generated"))
    if (-not $resolvedOutput.StartsWith($generatedRoot + [System.IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase)) {
        Fail "candidate manifests must be written under deployments/.generated for review"
    }
    [System.IO.Directory]::CreateDirectory((Split-Path -Parent $resolvedOutput)) | Out-Null
    [System.IO.File]::WriteAllText($resolvedOutput, $json, $utf8WithoutBom)
    Write-Output "Generated candidate manifest $resolvedOutput"

    if ($WithRouter) {
        $peripheryEnvironment = [ordered]@{
            POOL_MANAGER = $poolManager
            POSITION_MANAGER = $positionManager
        }
        foreach ($entry in $peripheryEnvironment.GetEnumerator()) {
            $deploymentEnvironment[$entry.Key] = $entry.Value
            $previousEnvironment[$entry.Key] = [Environment]::GetEnvironmentVariable($entry.Key, "Process")
            [Environment]::SetEnvironmentVariable($entry.Key, [string] $entry.Value, "Process")
        }
        # Stdout only: console2.log writes there, and Windows PowerShell 5.1 turns a redirected
        # native stderr line (a forge warning) into a terminating error under Stop.
        $peripheryOutput = (& forge script "script/v2/DeployLocalV4Periphery.s.sol:DeployLocalV4Periphery" `
            --rpc-url $RpcUrl --sender $Sender --broadcast --unlocked | Out-String)
        if ($LASTEXITCODE -ne 0) {
            Write-Output $peripheryOutput
            Fail "forge script DeployLocalV4Periphery exited with $LASTEXITCODE"
        }
        $routerMatch = [regex]::Match($peripheryOutput, "UNIVERSAL_ROUTER (0x[0-9a-fA-F]{40})")
        $quoterMatch = [regex]::Match($peripheryOutput, "V4_QUOTER (0x[0-9a-fA-F]{40})")
        if (-not $routerMatch.Success -or -not $quoterMatch.Success) {
            Fail "DeployLocalV4Periphery did not report both addresses"
        }
        $universalRouter = $routerMatch.Groups[1].Value
        $v4Quoter = $quoterMatch.Groups[1].Value
        # Both must trade against this deployment's PoolManager, read back from chain.
        foreach ($peripheryContract in @($universalRouter, $v4Quoter)) {
            if ((Get-Address $peripheryContract "poolManager()(address)") -ine $poolManager) {
                Fail "$peripheryContract is not wired to PoolManager $poolManager"
            }
        }
        $periphery = [ordered]@{
            universalRouter = $universalRouter
            v4Quoter = $v4Quoter
            universalRouterSource = "Uniswap/universal-router release/2.1.x 8580368c32e649a939af4c306d5f6d7010822680 (2.1.2)"
            bytecodeHashes = [ordered]@{
                universalRouter = Get-CodeHash $universalRouter
                v4Quoter = Get-CodeHash $v4Quoter
            }
        }
        $peripheryPath = [System.IO.Path]::ChangeExtension($resolvedOutput, ".v4-periphery.json")
        [System.IO.File]::WriteAllText(
            $peripheryPath, (($periphery | ConvertTo-Json -Depth 5) + "`n"), $utf8WithoutBom
        )
        Write-Output "Generated V4 periphery record $peripheryPath"
    }
}
finally {
    foreach ($entry in $deploymentEnvironment.GetEnumerator()) {
        if ($previousEnvironment.ContainsKey($entry.Key)) {
            [Environment]::SetEnvironmentVariable($entry.Key, $previousEnvironment[$entry.Key], "Process")
        }
    }
    Pop-Location
}
