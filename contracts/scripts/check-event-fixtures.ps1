param([switch] $Write)

$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest

$contractsRoot = Split-Path -Parent $PSScriptRoot
$generatedRoot = Join-Path $contractsRoot "fixtures/.generated"
$fixtures = @(
    [PSCustomObject]@{
        Name = "v1"
        Script = "script/GenerateEventFixtures.s.sol:GenerateEventFixtures"
        Variable = "EVENT_FIXTURES_OUTPUT"
        Artifact = "fixtures/v1/event-logs-v1.json"
    },
    [PSCustomObject]@{
        Name = "v2"
        Script = "script/v2/GenerateEventFixturesV2.s.sol:GenerateEventFixturesV2"
        Variable = "EVENT_FIXTURES_V2_OUTPUT"
        Artifact = "fixtures/v2/event-logs-v2.json"
    }
)

function Fail([string] $Message) { throw "Event fixture check failed: $Message" }

Push-Location $contractsRoot
try {
    [System.IO.Directory]::CreateDirectory($generatedRoot) | Out-Null
    foreach ($fixture in $fixtures) {
        $artifactPath = Join-Path $contractsRoot $fixture.Artifact
        $generatedPath = Join-Path $generatedRoot (Split-Path -Leaf $fixture.Artifact)
        $previousOutput = [Environment]::GetEnvironmentVariable($fixture.Variable, "Process")
        try {
            [Environment]::SetEnvironmentVariable($fixture.Variable, $generatedPath, "Process")
            & forge script $fixture.Script --silent
            if ($LASTEXITCODE -ne 0) { Fail "$($fixture.Name) generator exited with $LASTEXITCODE" }
        }
        finally {
            [Environment]::SetEnvironmentVariable($fixture.Variable, $previousOutput, "Process")
        }
        if (-not (Test-Path -LiteralPath $generatedPath)) {
            Fail "$($fixture.Name) generator wrote no artifact"
        }

        if ($Write) {
            [System.IO.Directory]::CreateDirectory((Split-Path -Parent $artifactPath)) | Out-Null
            Copy-Item -LiteralPath $generatedPath -Destination $artifactPath -Force
            Write-Output "Wrote $artifactPath"
            continue
        }
        if (-not (Test-Path -LiteralPath $artifactPath)) {
            Fail "missing $($fixture.Artifact); run ./scripts/check-event-fixtures.ps1 -Write"
        }
        $expected = [System.IO.File]::ReadAllBytes($artifactPath)
        $actual = [System.IO.File]::ReadAllBytes($generatedPath)
        # Base64 equality is an exact byte comparison that also parses on Windows PowerShell 5.1.
        if ([Convert]::ToBase64String($expected) -cne [Convert]::ToBase64String($actual)) {
            Fail "$($fixture.Artifact) has drifted"
        }
    }
    if (-not $Write) { Write-Output "Event fixtures verified." }
}
finally {
    if (Test-Path -LiteralPath $generatedRoot) {
        Remove-Item -LiteralPath $generatedRoot -Recurse -Force
    }
    Pop-Location
}
