param(
    [string] $RpcUrl = $env:ROBINHOOD_MAINNET_ARCHIVE_RPC_URL
)

$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest

$contractsRoot = Split-Path -Parent $PSScriptRoot
$configPath = Join-Path $contractsRoot "deployments/config/robinhood-mainnet.json"
$config = Get-Content -Raw -LiteralPath $configPath | ConvertFrom-Json
$v4ConfigPath = Join-Path $contractsRoot "deployments/config/robinhood-mainnet-v4.json"
$v4Config = Get-Content -Raw -LiteralPath $v4ConfigPath | ConvertFrom-Json

function Fail([string] $Message) {
    throw "Robinhood mainnet fork check failed: $Message"
}

function Invoke-Text([string] $Command, [string[]] $Arguments) {
    $value = (& $Command @Arguments | Out-String).Trim()
    if ($LASTEXITCODE -ne 0) { Fail "$Command exited with $LASTEXITCODE" }
    return $value
}

if ([string]::IsNullOrWhiteSpace($RpcUrl)) {
    Fail "ROBINHOOD_MAINNET_ARCHIVE_RPC_URL is required; the fork test is never skipped"
}

$rpcUri = [Uri] $RpcUrl
$quickNodeHostSuffix = ".robinhood-mainnet.quiknode.pro"
if (
    $rpcUri.Scheme -ne "https" -or
    $rpcUri.Host.Length -le $quickNodeHostSuffix.Length -or
    -not $rpcUri.Host.EndsWith(
        $quickNodeHostSuffix,
        [StringComparison]::OrdinalIgnoreCase
    )
) {
    Fail "the recorded Task 11 archive provider is QuickNode; configure its HTTPS Robinhood mainnet endpoint"
}

$chainId = Invoke-Text "cast" @("chain-id", "--rpc-url", $RpcUrl)
if ($chainId -ne "4663") { Fail "expected chain id 4663, got $chainId" }

$forkBlock = [uint64] $config.forkVerification.blockNumber
$blockHash = Invoke-Text "cast" @(
    "block", [string] $forkBlock, "--field", "hash", "--rpc-url", $RpcUrl
)
if ($blockHash -cne [string] $config.forkVerification.blockHash) {
    Fail "fork block hash does not match the reviewed evidence"
}

foreach ($dependency in @(
    @{ Name = "WETH"; Address = [string] $config.weth; Hash = [string] $config.bytecodeHashes.weth },
    @{
        Name = "Uniswap v2 Factory"
        Address = [string] $config.uniswapV2Factory
        Hash = [string] $config.bytecodeHashes.uniswapV2Factory
    }
)) {
    $runtimeCode = Invoke-Text "cast" @(
        "code", $dependency.Address, "--block", [string] $forkBlock,
        "--rpc-url", $RpcUrl
    )
    if ($runtimeCode -eq "0x") {
        Fail "$($dependency.Name) has no runtime code at the fork block"
    }
    $actual = Invoke-Text "cast" @("keccak", $runtimeCode)
    if ($actual -cne $dependency.Hash) {
        Fail "$($dependency.Name) runtime code hash does not match the reviewed evidence"
    }
}

# v2 runs at its own reviewed block: the Uniswap v4 record was pinned after the v1 record.
$v4ForkBlock = [uint64] $v4Config.forkVerification.blockNumber
$v4BlockHash = Invoke-Text "cast" @(
    "block", [string] $v4ForkBlock, "--field", "hash", "--rpc-url", $RpcUrl
)
if ($v4BlockHash -cne [string] $v4Config.forkVerification.blockHash) {
    Fail "v4 fork block hash does not match the reviewed evidence"
}
foreach ($name in @("poolManager", "positionManager", "permit2", "universalRouter")) {
    $actual = Invoke-Text "cast" @(
        "codehash", [string] $v4Config.$name, "--block", [string] $v4ForkBlock,
        "--rpc-url", $RpcUrl
    )
    if ($actual -cne [string] $v4Config.bytecodeHashes.$name) {
        Fail "Uniswap v4 $name runtime code hash does not match the reviewed evidence"
    }
}

$previousProfile = [Environment]::GetEnvironmentVariable(
    "FOUNDRY_PROFILE", [EnvironmentVariableTarget]::Process
)
[Environment]::SetEnvironmentVariable(
    "FOUNDRY_PROFILE", "fork", [EnvironmentVariableTarget]::Process
)
Push-Location $contractsRoot
try {
    & forge test --fork-url $RpcUrl --fork-block-number $forkBlock `
        --no-match-contract "RobinhoodMainnetV2ForkTest" -vvv
    if ($LASTEXITCODE -ne 0) { Fail "forge v1 fork suite exited with $LASTEXITCODE" }
    & forge test --fork-url $RpcUrl --fork-block-number $v4ForkBlock `
        --match-contract "RobinhoodMainnetV2ForkTest" -vvv
    if ($LASTEXITCODE -ne 0) { Fail "forge v2 fork suite exited with $LASTEXITCODE" }
}
finally {
    Pop-Location
    [Environment]::SetEnvironmentVariable(
        "FOUNDRY_PROFILE", $previousProfile, [EnvironmentVariableTarget]::Process
    )
}

Write-Output "Robinhood mainnet fork compatibility verified at blocks $forkBlock (v1) and $v4ForkBlock (v2) via QuickNode."
