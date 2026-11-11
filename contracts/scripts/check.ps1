param(
    [ValidateSet("all", "release", "pins", "fmt", "build", "goldens", "vectors", "event-fixtures", "deployments", "simulation", "review", "test", "lint", "size", "slither", "fork", "runner-test")]
    [string] $Target = "all"
)

$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest

$contractsRoot = Split-Path -Parent $PSScriptRoot
Push-Location $contractsRoot
try {
    . (Join-Path $PSScriptRoot "check-runner.ps1")

    function Invoke-WithFoundryProfile([string] $Profile, [scriptblock] $Command) {
        $hadPreviousProfile = Test-Path Env:FOUNDRY_PROFILE
        $previousProfile = [Environment]::GetEnvironmentVariable(
            "FOUNDRY_PROFILE",
            [EnvironmentVariableTarget]::Process
        )
        try {
            [Environment]::SetEnvironmentVariable(
                "FOUNDRY_PROFILE",
                $Profile,
                [EnvironmentVariableTarget]::Process
            )
            Invoke-Checked $Command
        }
        finally {
            if ($hadPreviousProfile) {
                [Environment]::SetEnvironmentVariable(
                    "FOUNDRY_PROFILE",
                    $previousProfile,
                    [EnvironmentVariableTarget]::Process
                )
            }
            else {
                Remove-Item Env:FOUNDRY_PROFILE -ErrorAction SilentlyContinue
            }
        }
    }

    # Every target that compiles tests or scripts needs the test-only Uniswap v4 artifacts
    # (`out-v4/`), built from the pinned submodule with the upstream compiler settings.
    if ($Target -notin @("pins", "fmt", "runner-test")) {
        Invoke-WithFoundryProfile "v4" { forge build --no-lint }
    }
    if ($Target -in @("all", "release", "pins")) {
        Invoke-Checked { & "$PSScriptRoot/check-dependencies.ps1" }
    }
    if ($Target -in @("all", "release", "runner-test")) {
        Invoke-Checked { & "$PSScriptRoot/test-check-runner.ps1" }
    }
    if ($Target -in @("all", "release", "fmt")) {
        Invoke-Checked { forge fmt --check }
    }
    if ($Target -in @("all", "release", "build")) {
        Invoke-Checked { forge build --no-lint }
        Invoke-WithFoundryProfile "fork" { forge build --no-lint }
    }
    if ($Target -in @("all", "release", "goldens")) {
        Invoke-Checked { & "$PSScriptRoot/check-goldens.ps1" }
    }
    if ($Target -in @("all", "release", "vectors")) {
        Invoke-Checked { & "$PSScriptRoot/check-vectors.ps1" }
    }
    if ($Target -in @("all", "release", "event-fixtures")) {
        Invoke-Checked { & "$PSScriptRoot/check-event-fixtures.ps1" }
    }
    if ($Target -in @("all", "release", "deployments")) {
        Invoke-Checked { & "$PSScriptRoot/check-deployments.ps1" }
    }
    if ($Target -in @("release", "simulation")) {
        Invoke-Checked { & "$PSScriptRoot/check-deployment-simulation.ps1" }
    }
    if ($Target -in @("all", "release", "review")) {
        Invoke-Checked { & "$PSScriptRoot/check-release.ps1" }
    }
    if ($Target -in @("all", "release", "test")) {
        Invoke-Checked { forge test }
    }
    if ($Target -in @("all", "release", "lint")) {
        Invoke-Checked { forge lint --severity high med low info -D warnings }
    }
    if ($Target -in @("all", "release", "size")) {
        Invoke-Checked { & "$PSScriptRoot/check-sizes.ps1" }
    }
    if ($Target -in @("release", "slither")) {
        Invoke-Checked { & "$PSScriptRoot/check-slither.ps1" }
    }
    if ($Target -in @("release", "fork")) {
        Invoke-Checked { & "$PSScriptRoot/check-fork.ps1" }
    }
}
finally {
    Pop-Location
}
