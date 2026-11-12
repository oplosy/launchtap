$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest

$contractsRoot = Split-Path -Parent $PSScriptRoot
$repositoryRoot = Split-Path -Parent $contractsRoot
$lockPath = Join-Path $contractsRoot "dependencies.lock"
$foundryLockPath = Join-Path $contractsRoot "foundry.lock"
$workflowPath = Join-Path $repositoryRoot ".github/workflows/contracts.yml"

function Fail([string] $Message) {
    throw "Dependency pin check failed: $Message"
}

$pins = @{}
foreach ($line in Get-Content -LiteralPath $lockPath) {
    $trimmed = $line.Trim()
    if ($trimmed.Length -eq 0 -or $trimmed.StartsWith("#")) {
        continue
    }

    $parts = $trimmed.Split("=", 2)
    if ($parts.Count -ne 2 -or $pins.ContainsKey($parts[0])) {
        Fail "invalid or duplicate lock entry '$trimmed'"
    }
    $pins[$parts[0]] = $parts[1]
}

$requiredPins = @(
    "FOUNDRY_VERSION",
    "FOUNDRY_COMMIT",
    "FOUNDRY_TOOLCHAIN_ACTION_VERSION",
    "FOUNDRY_TOOLCHAIN_ACTION_COMMIT",
    "OPENZEPPELIN_VERSION",
    "OPENZEPPELIN_COMMIT",
    "FORGE_STD_VERSION",
    "FORGE_STD_COMMIT",
    "UNISWAP_V4_PERIPHERY_COMMIT",
    "UNISWAP_V4_CORE_VERSION",
    "UNISWAP_V4_CORE_COMMIT",
    "PERMIT2_COMMIT",
    "UNISWAP_UNIVERSAL_ROUTER_COMMIT",
    "UNISWAP_UNIVERSAL_ROUTER_V4_PERIPHERY_COMMIT",
    "UNISWAP_V2_CORE_VERSION",
    "UNISWAP_V2_CORE_COMMIT",
    "UNISWAP_V3_CORE_VERSION",
    "UNISWAP_V3_CORE_COMMIT"
)
foreach ($requiredPin in $requiredPins) {
    if (-not $pins.ContainsKey($requiredPin)) {
        Fail "missing $requiredPin"
    }
}

$forge = Get-Command forge -ErrorAction SilentlyContinue
if ($null -eq $forge) {
    Fail "forge is not on PATH"
}

$forgeVersion = (& forge --version | Out-String)
if ($LASTEXITCODE -ne 0) {
    Fail "forge --version exited with $LASTEXITCODE"
}
$expectedVersion = $pins["FOUNDRY_VERSION"].TrimStart("v")
if ($forgeVersion -notmatch "Version:\s+$([regex]::Escape($expectedVersion))(?:[-+][^\s]+)?(\s|$)") {
    Fail "expected Foundry $($pins['FOUNDRY_VERSION'])"
}
if ($forgeVersion -notmatch "Commit SHA:\s+$([regex]::Escape($pins['FOUNDRY_COMMIT']))(\s|$)") {
    Fail "expected Foundry commit $($pins['FOUNDRY_COMMIT'])"
}

$dependencies = @(
    @{
        Name = "OpenZeppelin Contracts"
        Path = Join-Path $contractsRoot "lib/openzeppelin-contracts"
        Submodule = "contracts/lib/openzeppelin-contracts"
        Url = "https://github.com/OpenZeppelin/openzeppelin-contracts.git"
        Commit = $pins["OPENZEPPELIN_COMMIT"]
    },
    @{
        Name = "forge-std"
        Path = Join-Path $contractsRoot "lib/forge-std"
        Submodule = "contracts/lib/forge-std"
        Url = "https://github.com/foundry-rs/forge-std.git"
        Commit = $pins["FORGE_STD_COMMIT"]
    },
    @{
        Name = "Uniswap v4-periphery"
        Path = Join-Path $contractsRoot "lib/v4-periphery"
        Submodule = "contracts/lib/v4-periphery"
        Url = "https://github.com/Uniswap/v4-periphery.git"
        Commit = $pins["UNISWAP_V4_PERIPHERY_COMMIT"]
    },
    # Nested submodules of v4-periphery: their URLs live in v4-periphery's own .gitmodules.
    @{
        Name = "Uniswap v4-core"
        Path = Join-Path $contractsRoot "lib/v4-periphery/lib/v4-core"
        GitmodulesRoot = Join-Path $contractsRoot "lib/v4-periphery"
        Submodule = "lib/v4-core"
        Url = "https://github.com/Uniswap/v4-core"
        Commit = $pins["UNISWAP_V4_CORE_COMMIT"]
    },
    @{
        Name = "Permit2"
        Path = Join-Path $contractsRoot "lib/v4-periphery/lib/permit2"
        GitmodulesRoot = Join-Path $contractsRoot "lib/v4-periphery"
        Submodule = "lib/permit2"
        Url = "https://github.com/Uniswap/permit2"
        Commit = $pins["PERMIT2_COMMIT"]
    },
    @{
        Name = "Uniswap Universal Router"
        Path = Join-Path $contractsRoot "lib/universal-router"
        Submodule = "contracts/lib/universal-router"
        Url = "https://github.com/Uniswap/universal-router.git"
        Commit = $pins["UNISWAP_UNIVERSAL_ROUTER_COMMIT"]
    },
    # The router's own v4-periphery decides the V4 swap encoding the browser signs (Plan v2-C D2).
    @{
        Name = "Universal Router v4-periphery"
        Path = Join-Path $contractsRoot "lib/universal-router/lib/v4-periphery"
        GitmodulesRoot = Join-Path $contractsRoot "lib/universal-router"
        Submodule = "lib/v4-periphery"
        Url = "https://github.com/Uniswap/v4-periphery"
        Commit = $pins["UNISWAP_UNIVERSAL_ROUTER_V4_PERIPHERY_COMMIT"]
    },
    @{
        Name = "Uniswap v2-core"
        Path = Join-Path $contractsRoot "lib/uniswap-v2-core"
        Submodule = "contracts/lib/uniswap-v2-core"
        Url = "https://github.com/Uniswap/v2-core.git"
        Commit = $pins["UNISWAP_V2_CORE_COMMIT"]
    },
    @{
        Name = "Uniswap v3-core"
        Path = Join-Path $contractsRoot "lib/uniswap-v3-core"
        Submodule = "contracts/lib/uniswap-v3-core"
        Url = "https://github.com/Uniswap/v3-core.git"
        Commit = $pins["UNISWAP_V3_CORE_COMMIT"]
    }
)

foreach ($dependency in $dependencies) {
    if (-not (Test-Path -LiteralPath $dependency.Path)) {
        Fail "$($dependency.Name) is not installed"
    }

    $actualCommit = (& git -C $dependency.Path rev-parse HEAD).Trim()
    if ($LASTEXITCODE -ne 0 -or $actualCommit -ne $dependency.Commit) {
        Fail "$($dependency.Name) expected $($dependency.Commit), got $actualCommit"
    }

    # Universal Router nests submodules deep enough to exceed the Windows path limit;
    # core.longpaths reaches every nested git status (no effect on other platforms).
    $dirty = (& git -c core.longpaths=true -C $dependency.Path status --porcelain | Out-String).Trim()
    if ($LASTEXITCODE -ne 0 -or $dirty.Length -ne 0) {
        Fail "$($dependency.Name) has local changes"
    }

    $urlKey = "submodule.$($dependency.Submodule).url"
    $gitmodulesRoot = $repositoryRoot
    if ($dependency.ContainsKey("GitmodulesRoot")) {
        $gitmodulesRoot = $dependency.GitmodulesRoot
    }
    $actualUrl = (& git -C $gitmodulesRoot config -f .gitmodules --get $urlKey).Trim()
    if ($LASTEXITCODE -ne 0 -or $actualUrl -ne $dependency.Url) {
        Fail "$($dependency.Name) expected URL $($dependency.Url), got $actualUrl"
    }
}

$foundryLock = Get-Content -Raw -LiteralPath $foundryLockPath | ConvertFrom-Json
$foundryDependencies = @(
    @{
        Path = "lib/openzeppelin-contracts"
        Version = $pins["OPENZEPPELIN_VERSION"]
        Commit = $pins["OPENZEPPELIN_COMMIT"]
    },
    @{
        Path = "lib/forge-std"
        Version = $pins["FORGE_STD_VERSION"]
        Commit = $pins["FORGE_STD_COMMIT"]
    },
    @{
        Path = "lib/v4-periphery"
        Version = $null
        Commit = $pins["UNISWAP_V4_PERIPHERY_COMMIT"]
    },
    @{
        Path = "lib/universal-router"
        Version = $null
        Commit = $pins["UNISWAP_UNIVERSAL_ROUTER_COMMIT"]
    },
    @{
        Path = "lib/uniswap-v2-core"
        Version = $pins["UNISWAP_V2_CORE_VERSION"]
        Commit = $pins["UNISWAP_V2_CORE_COMMIT"]
    },
    @{
        Path = "lib/uniswap-v3-core"
        Version = $pins["UNISWAP_V3_CORE_VERSION"]
        Commit = $pins["UNISWAP_V3_CORE_COMMIT"]
    }
)
foreach ($dependency in $foundryDependencies) {
    $entryProperty = $foundryLock.PSObject.Properties[$dependency.Path]
    if ($null -eq $entryProperty) {
        Fail "foundry.lock is missing $($dependency.Path)"
    }

    $entry = $entryProperty.Value
    if ($null -eq $dependency.Version) {
        # Untagged dependency: foundry.lock pins the bare revision.
        if ($null -eq $entry.PSObject.Properties["rev"] -or $entry.rev -ne $dependency.Commit) {
            Fail "foundry.lock drift for $($dependency.Path)"
        }
        continue
    }
    if ($entry.tag.name -ne $dependency.Version -or $entry.tag.rev -ne $dependency.Commit) {
        Fail "foundry.lock drift for $($dependency.Path)"
    }
}

$workflow = Get-Content -Raw -LiteralPath $workflowPath
$actionReference = "foundry-rs/foundry-toolchain@$($pins['FOUNDRY_TOOLCHAIN_ACTION_COMMIT'])"
if (-not $workflow.Contains($actionReference)) {
    Fail "contract CI does not pin $actionReference"
}
$versionInput = "version: $($pins['FOUNDRY_VERSION'])"
if (-not $workflow.Contains($versionInput)) {
    Fail "contract CI does not select $versionInput"
}

Write-Output "Dependency pins verified."
