param(
    [Parameter(Mandatory = $true)]
    [string] $RpcUrl,

    [Parameter(Mandatory = $true)]
    [ValidatePattern("^0x[0-9a-fA-F]{40}$")]
    [string] $Sender,

    [switch] $Broadcast,
    [switch] $Unlocked,
    [string] $Account
)

# Deploys the pinned Uniswap v4 stack on Robinhood testnet (46630) for the engine-2 rehearsal and
# writes an unreviewed candidate record to deployments/.generated/robinhood-testnet-v4.json.
# Decisions: docs/specs/2026-11-18-v2-testnet-path-preflight.md. Without -Broadcast it verifies
# Permit2 and simulates the deployment against the live chain; nothing is sent.

$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest

$contractsRoot = Split-Path -Parent $PSScriptRoot
$deploymentsRoot = Join-Path $contractsRoot "deployments"
$candidatePath = Join-Path $deploymentsRoot ".generated/robinhood-testnet-v4.json"
$utf8WithoutBom = New-Object System.Text.UTF8Encoding($false)
$chainId = 46630
$permit2Address = "0x000000000022D473030F116dDEE9F6B43aC78BA3"
# The pinned Permit2 helper runtime was compiled on chain 31337; these are its two immutables.
$helperChainIdWord = "0000000000000000000000000000000000000000000000000000000000007a69"
$helperDomainSeparatorWord = "d5a17abc3865df5c1400c0299bd4ce2eefc8114aec5f9d3dded1745783e57b98"

function Fail([string] $Message) {
    throw "Testnet v4 bootstrap failed: $Message"
}

function Invoke-Cast([string[]] $Arguments) {
    $value = (& cast @Arguments --rpc-url $RpcUrl | Out-String).Trim()
    if ($LASTEXITCODE -ne 0) { Fail "cast $($Arguments -join ' ') exited with $LASTEXITCODE" }
    return $value
}

function Invoke-CastLocal([string[]] $Arguments) {
    $value = (& cast @Arguments | Out-String).Trim()
    if ($LASTEXITCODE -ne 0) { Fail "cast $($Arguments -join ' ') exited with $LASTEXITCODE" }
    return $value
}

function Get-Address([string] $Target, [string] $Signature) {
    $value = Invoke-Cast @("call", $Target, $Signature)
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

function Get-CreationReceipt([object] $Record, [string] $ContractAddress) {
    $found = @(
        $Record.receipts | Where-Object { [string] $_.contractAddress -ieq $ContractAddress }
    )
    if ($found.Count -ne 1) {
        Fail "expected exactly one creation receipt for $ContractAddress, found $($found.Count)"
    }
    return $found[0]
}

function Get-SingleIndex([string] $Haystack, [string] $Needle, [string] $Label) {
    $first = $Haystack.IndexOf($Needle, [StringComparison]::Ordinal)
    if ($first -lt 0 -or $Haystack.IndexOf($Needle, $first + 1, [StringComparison]::Ordinal) -ge 0) {
        Fail "the pinned Permit2 runtime must contain the $Label immutable exactly once"
    }
    return $first
}

# The live Permit2 must be the pinned canonical runtime with only its own chain id and
# domain separator in the two immutable words.
function Assert-CanonicalPermit2 {
    $helper = Join-Path $contractsRoot "lib/v4-periphery/lib/permit2/test/utils/DeployPermit2.sol"
    if (-not (Test-Path -LiteralPath $helper)) { Fail "pinned Permit2 helper $helper not found" }
    $runtime = [regex]::Match([System.IO.File]::ReadAllText($helper), 'hex"([0-9a-fA-F]+)"')
    if (-not $runtime.Success) { Fail "Permit2 runtime bytecode not found in $helper" }
    $expected = $runtime.Groups[1].Value.ToLowerInvariant()

    $typeHash = Invoke-CastLocal @("keccak", "EIP712Domain(string name,uint256 chainId,address verifyingContract)")
    $nameHash = Invoke-CastLocal @("keccak", "Permit2")
    $encoded = Invoke-CastLocal @(
        "abi-encode", "f(bytes32,bytes32,uint256,address)", $typeHash, $nameHash, [string] $chainId,
        $permit2Address
    )
    $domainSeparator = (Invoke-CastLocal @("keccak", $encoded)).ToLowerInvariant()
    $liveSeparator = (Invoke-Cast @("call", $permit2Address, "DOMAIN_SEPARATOR()(bytes32)")).ToLowerInvariant()
    if ($liveSeparator -ne $domainSeparator) {
        Fail "Permit2 DOMAIN_SEPARATOR $liveSeparator is not the EIP-712 separator $domainSeparator"
    }

    $chainIndex = Get-SingleIndex $expected ("7f" + $helperChainIdWord) "chain id"
    $separatorIndex = Get-SingleIndex $expected ("7f" + $helperDomainSeparatorWord) "domain separator"
    $chainWord = ([Convert]::ToString($chainId, 16)).PadLeft(64, "0")
    $expected = $expected.Remove($chainIndex + 2, 64).Insert($chainIndex + 2, $chainWord)
    $expected = $expected.Remove($separatorIndex + 2, 64).Insert($separatorIndex + 2, $domainSeparator.Substring(2))

    $live = (Invoke-Cast @("code", $permit2Address)).ToLowerInvariant()
    if ($live -ne ("0x" + $expected)) {
        Fail "Permit2 at $permit2Address is not the pinned canonical runtime for chain $chainId"
    }
    Write-Output "Permit2 verified: pinned canonical runtime with the chain $chainId immutables."
}

function Invoke-WithProfile([string] $ProfileName, [scriptblock] $Block) {
    $previous = [Environment]::GetEnvironmentVariable("FOUNDRY_PROFILE", "Process")
    [Environment]::SetEnvironmentVariable("FOUNDRY_PROFILE", $ProfileName, "Process")
    try { & $Block } finally { [Environment]::SetEnvironmentVariable("FOUNDRY_PROFILE", $previous, "Process") }
}

if ($Unlocked -and -not [string]::IsNullOrWhiteSpace($Account)) {
    Fail "choose either -Unlocked or -Account, not both"
}
if ($Broadcast -and -not $Unlocked -and [string]::IsNullOrWhiteSpace($Account)) {
    Fail "broadcast requires -Unlocked or a named Foundry -Account"
}

$observedChainId = Invoke-Cast @("chain-id")
if ($observedChainId -ne [string] $chainId) {
    Fail "expected Robinhood testnet chain id $chainId, got $observedChainId"
}

$previousDeployer = [Environment]::GetEnvironmentVariable("DEPLOYER", "Process")
[Environment]::SetEnvironmentVariable("DEPLOYER", $Sender, "Process")
Push-Location $contractsRoot
try {
    Assert-CanonicalPermit2

    foreach ($buildProfile in @("v4", "router")) {
        Invoke-WithProfile $buildProfile { & forge build --no-lint | Out-Null }
        if ($LASTEXITCODE -ne 0) { Fail "forge build for profile $buildProfile exited with $LASTEXITCODE" }
    }

    $arguments = @(
        "script", "script/v2/DeployTestnetV4.s.sol:DeployTestnetV4",
        "--rpc-url", $RpcUrl, "--sender", $Sender
    )
    if ($Broadcast) {
        $arguments += "--broadcast"
        if ($Unlocked) { $arguments += "--unlocked" } else { $arguments += @("--account", $Account) }
    }
    # Stdout only: Windows PowerShell 5.1 turns a redirected native stderr line into an error.
    $output = (& forge @arguments | Out-String)
    if ($LASTEXITCODE -ne 0) {
        Write-Output $output
        Fail "forge script DeployTestnetV4 exited with $LASTEXITCODE"
    }
    $addresses = [ordered]@{}
    foreach ($name in @("POOL_MANAGER", "POSITION_MANAGER", "V4_QUOTER", "UNIVERSAL_ROUTER")) {
        $match = [regex]::Match($output, "$name (0x[0-9a-fA-F]{40})")
        if (-not $match.Success) { Fail "DeployTestnetV4 did not report $name" }
        $addresses[$name] = $match.Groups[1].Value
    }

    if (-not $Broadcast) {
        Write-Output $output
        Write-Output "Testnet v4 bootstrap simulation passed. Nothing was broadcast."
        return
    }

    $broadcastPath = Join-Path $contractsRoot "broadcast/DeployTestnetV4.s.sol/$chainId/run-latest.json"
    if (-not (Test-Path -LiteralPath $broadcastPath)) { Fail "missing Foundry broadcast record $broadcastPath" }
    $record = Get-Content -Raw -LiteralPath $broadcastPath | ConvertFrom-Json

    $poolManager = $addresses["POOL_MANAGER"]
    $positionManager = $addresses["POSITION_MANAGER"]
    $v4Quoter = $addresses["V4_QUOTER"]
    $universalRouter = $addresses["UNIVERSAL_ROUTER"]

    # Read every wire back from chain, so the record reflects what is actually deployed.
    $reviewedTestnet = Get-Content -Raw -LiteralPath (Join-Path $deploymentsRoot "config/robinhood-testnet.json") |
        ConvertFrom-Json
    if ((Get-Address $positionManager "poolManager()(address)") -ine $poolManager) {
        Fail "PositionManager is wired to another PoolManager"
    }
    if ((Get-Address $positionManager "permit2()(address)") -ine $permit2Address) {
        Fail "PositionManager is wired to another Permit2"
    }
    $weth9 = Get-Address $positionManager "WETH9()(address)"
    if ($weth9 -ine [string] $reviewedTestnet.weth) { Fail "PositionManager WETH9 is not the reviewed testnet WETH" }
    foreach ($peripheryContract in @($v4Quoter, $universalRouter)) {
        if ((Get-Address $peripheryContract "poolManager()(address)") -ine $poolManager) {
            Fail "$peripheryContract is not wired to PoolManager $poolManager"
        }
    }
    $poolManagerOwner = Get-Address $poolManager "owner()(address)"
    if ($poolManagerOwner -ine $Sender) { Fail "PoolManager owner $poolManagerOwner is not the deployer" }

    $blockNumber = [uint64] (Invoke-Cast @("block-number"))
    $blockHash = (Invoke-Cast @("block", [string] $blockNumber, "--field", "hash")).ToLowerInvariant()

    $candidate = [ordered]@{
        schemaVersion = 1
        provenance = "self-deployed"
        target = "robinhood-testnet"
        chainId = $chainId
        reviewed = $false
        poolManager = $poolManager
        positionManager = $positionManager
        permit2 = $permit2Address
        universalRouter = $universalRouter
        positionDescriptor = $null
        stateView = $null
        v4Quoter = $v4Quoter
        weth9 = $weth9
        poolManagerOwner = $poolManagerOwner
        deployTransactions = [ordered]@{
            poolManager = [string] (Get-CreationReceipt $record $poolManager).transactionHash
            positionManager = [string] (Get-CreationReceipt $record $positionManager).transactionHash
            v4Quoter = [string] (Get-CreationReceipt $record $v4Quoter).transactionHash
            universalRouter = [string] (Get-CreationReceipt $record $universalRouter).transactionHash
        }
        bytecodeHashes = [ordered]@{
            poolManager = Get-CodeHash $poolManager
            positionManager = Get-CodeHash $positionManager
            permit2 = Get-CodeHash $permit2Address
            universalRouter = Get-CodeHash $universalRouter
            v4Quoter = Get-CodeHash $v4Quoter
        }
        forkVerification = [ordered]@{
            blockNumber = $blockNumber
            blockHash = $blockHash
            observedRpcProvider = "Robinhood Public RPC"
            archiveRpcProvider = "Robinhood Public RPC"
            rpcEnvironmentVariable = "ROBINHOOD_TESTNET_RPC_URL"
            archiveRequired = $false
        }
        evidence = [ordered]@{
            poolManager = "deployed by DeployTestnetV4 from the pinned out-v4 build (v4-core 59d3ecf5); owner is the testnet deployer"
            positionManager = "deployed by DeployTestnetV4 from the pinned out-v4 build (v4-periphery c6e4754d); no descriptor, reviewed testnet WETH9"
            permit2 = "pre-existing canonical Permit2: pinned runtime with the chain 46630 id and EIP-712 domain separator immutables, verified by bootstrap-testnet-v4.ps1"
            universalRouter = "deployed by DeployTestnetV4 from the pinned out-router build (2.1.2); unused protocols map to UnsupportedProtocol"
            v4Quoter = "deployed by DeployTestnetV4 from the pinned out-v4 build (v4-periphery c6e4754d)"
        }
        sources = [ordered]@{
            v4Core = (& git -C (Join-Path $contractsRoot "lib/v4-periphery/lib/v4-core") rev-parse HEAD).Trim()
            v4Periphery = (& git -C (Join-Path $contractsRoot "lib/v4-periphery") rev-parse HEAD).Trim()
            universalRouter = (& git -C (Join-Path $contractsRoot "lib/universal-router") rev-parse HEAD).Trim()
            permit2Runtime = "lib/v4-periphery/lib/permit2/test/utils/DeployPermit2.sol"
            chainRpc = "https://docs.robinhood.com/chain/connecting/"
        }
    }
    $json = ($candidate | ConvertTo-Json -Depth 10) + "`n"
    [System.IO.Directory]::CreateDirectory((Split-Path -Parent $candidatePath)) | Out-Null
    [System.IO.File]::WriteAllText($candidatePath, $json, $utf8WithoutBom)
    Write-Output "Generated unreviewed testnet v4 candidate $candidatePath"
}
finally {
    [Environment]::SetEnvironmentVariable("DEPLOYER", $previousDeployer, "Process")
    Pop-Location
}
