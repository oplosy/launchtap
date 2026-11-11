param(
    [switch] $Write
)

$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest

$contractsRoot = Split-Path -Parent $PSScriptRoot
$artifactPath = Join-Path $contractsRoot "vectors/v1/curve-v1.json"
$schemaPath = Join-Path $contractsRoot "vectors/v1/curve.schema.json"
$generatedDirectory = Join-Path $contractsRoot "vectors/.generated"
$generatedPath = Join-Path $generatedDirectory "curve-v1.json"
$outputPath = if ($Write) { "./vectors/v1/curve-v1.json" } else { "./vectors/.generated/curve-v1.json" }
$previousOutput = [Environment]::GetEnvironmentVariable("CURVE_VECTORS_OUTPUT", "Process")
$v2ArtifactPath = Join-Path $contractsRoot "vectors/v2/curve-v2.json"
$v2SchemaPath = Join-Path $contractsRoot "vectors/v2/curve.schema.json"
$v2GeneratedPath = Join-Path $generatedDirectory "curve-v2.json"
$v2OutputPath = if ($Write) { "./vectors/v2/curve-v2.json" } else { "./vectors/.generated/curve-v2.json" }
$previousV2Output = [Environment]::GetEnvironmentVariable("CURVE_VECTORS_V2_OUTPUT", "Process")

function Fail([string] $Message) {
    throw "Curve vector check failed: $Message"
}

function To-Amount([object] $Value) {
    $text = [string] $Value
    if ($text -notmatch '^(0|[1-9][0-9]*)$') {
        Fail "invalid uint256 decimal string: $text"
    }
    return [System.Numerics.BigInteger]::Parse($text)
}

function Assert-AmountFields([object] $Object, [string[]] $Fields, [string] $Context) {
    foreach ($field in $Fields) {
        if ($null -eq $Object.PSObject.Properties[$field]) {
            Fail "$Context is missing $field"
        }
        To-Amount $Object.$field | Out-Null
    }
}

function Assert-UnchangedState([object] $Case) {
    $initial = $Case.initialState | ConvertTo-Json -Depth 20 -Compress
    $next = $Case.nextState | ConvertTo-Json -Depth 20 -Compress
    if ($initial -ne $next) {
        Fail "$($Case.id) changed state on a reverting quote"
    }
}

Push-Location $contractsRoot
try {
    if (-not (Test-Path -LiteralPath $schemaPath)) {
        Fail "missing vectors/v1/curve.schema.json"
    }
    Get-Content -Raw -LiteralPath $schemaPath | ConvertFrom-Json | Out-Null

    if (-not $Write) {
        [System.IO.Directory]::CreateDirectory($generatedDirectory) | Out-Null
    }
    [Environment]::SetEnvironmentVariable("CURVE_VECTORS_OUTPUT", $outputPath, "Process")
    & forge script "script/GenerateCurveVectors.s.sol:GenerateCurveVectors" -q
    if ($LASTEXITCODE -ne 0) {
        Fail "Foundry generator exited with $LASTEXITCODE"
    }

    $candidatePath = if ($Write) { $artifactPath } else { $generatedPath }
    if (-not (Test-Path -LiteralPath $candidatePath)) {
        Fail "generator did not write $candidatePath"
    }
    $artifact = Get-Content -Raw -LiteralPath $candidatePath | ConvertFrom-Json

    if ($artifact.'$schema' -ne "./curve.schema.json") {
        Fail "artifact must reference ./curve.schema.json"
    }
    if ([int] $artifact.schemaVersion -ne 1 -or [int] $artifact.engineVersion -ne 1) {
        Fail "schemaVersion and engineVersion must both remain 1"
    }
    if ($artifact.amountEncoding -ne "uint256-decimal-string") {
        Fail "amountEncoding must remain uint256-decimal-string"
    }

    Assert-AmountFields $artifact.parameters @(
        "totalSupply",
        "curveTokens",
        "lpTokens",
        "graduationEth",
        "initialVirtualEth",
        "initialVirtualToken"
    ) "parameters"

    $expectedIds = @(
        "buy_normal",
        "buy_one_wei",
        "buy_fee_split_dust",
        "buy_mid_curve",
        "buy_just_below_graduation",
        "buy_final_exact",
        "buy_final_refund_and_graduation",
        "sell_normal",
        "sell_full",
        "invalid_buy_zero_input",
        "invalid_sell_zero_input",
        "invalid_sell_oversell",
        "invalid_sell_one_wei_zero_output"
    )
    $actualIds = @($artifact.cases | ForEach-Object { [string] $_.id })
    if (($actualIds -join "`n") -ne ($expectedIds -join "`n")) {
        Fail "case set or deterministic order has drifted"
    }

    $stateFields = @(
        "virtualEth",
        "virtualToken",
        "tokensSold",
        "realCurveEth",
        "protocolFees",
        "creatorFees"
    )
    $outputFields = @(
        "ethGross",
        "ethRefund",
        "ethOut",
        "tokenAmount",
        "protocolFee",
        "creatorFee"
    )
    foreach ($case in $artifact.cases) {
        Assert-AmountFields $case.initialState $stateFields "$($case.id).initialState"
        Assert-AmountFields $case.input @("ethGross", "tokensIn") "$($case.id).input"
        Assert-AmountFields $case.nextState $stateFields "$($case.id).nextState"

        if ($null -eq $case.expectedRevert) {
            if ($null -eq $case.output) {
                Fail "$($case.id) is missing a successful output"
            }
            Assert-AmountFields $case.output $outputFields "$($case.id).output"
            if ($case.operation -eq "buy") {
                $supplied = To-Amount $case.input.ethGross
                $consumed = (To-Amount $case.output.ethGross) + (To-Amount $case.output.ethRefund)
                if ($supplied -ne $consumed) {
                    Fail "$($case.id) does not conserve supplied buy ETH"
                }
            }
            elseif ($case.operation -eq "sell") {
                if ((To-Amount $case.output.ethRefund) -ne 0) {
                    Fail "$($case.id) records a sell refund"
                }
                if ((To-Amount $case.output.tokenAmount) -ne (To-Amount $case.input.tokensIn)) {
                    Fail "$($case.id) sell token amount differs from its input"
                }
            }
            else {
                Fail "$($case.id) has an unknown operation"
            }
        }
        else {
            if ($null -ne $case.output) {
                Fail "$($case.id) has both an output and an expected revert"
            }
            if ([string] $case.expectedRevert.data -notmatch '^0x[0-9a-f]+$') {
                Fail "$($case.id) has invalid revert data"
            }
            Assert-UnchangedState $case
        }
    }

    $oneWei = $artifact.cases | Where-Object id -eq "buy_one_wei"
    if ((To-Amount $oneWei.input.ethGross) -ne 1) {
        Fail "buy_one_wei no longer covers the one-wei buy boundary"
    }
    $feeDust = $artifact.cases | Where-Object id -eq "buy_fee_split_dust"
    if ((To-Amount $feeDust.output.protocolFee) -eq (To-Amount $feeDust.output.creatorFee)) {
        Fail "buy_fee_split_dust no longer exposes fee-split dust"
    }
    $finalExact = $artifact.cases | Where-Object id -eq "buy_final_exact"
    if (-not $finalExact.output.graduates -or (To-Amount $finalExact.output.ethRefund) -ne 0) {
        Fail "buy_final_exact must graduate without a refund"
    }
    $finalRefund = $artifact.cases | Where-Object id -eq "buy_final_refund_and_graduation"
    $finalRefundInvalid = -not $finalRefund.output.graduates
    $finalRefundInvalid = $finalRefundInvalid -or (To-Amount $finalRefund.output.ethRefund) -le 0
    $finalRefundInvalid = $finalRefundInvalid -or $finalRefund.nextState.phase -ne "graduated"
    if ($finalRefundInvalid) {
        Fail "buy_final_refund_and_graduation must refund and enter the graduated phase"
    }
    $midCurve = $artifact.cases | Where-Object id -eq "buy_mid_curve"
    if ((To-Amount $midCurve.initialState.tokensSold) -le 0) {
        Fail "buy_mid_curve must start from a non-genesis sold-token state"
    }
    $nearGraduation = $artifact.cases | Where-Object id -eq "buy_just_below_graduation"
    $nearGraduationInvalid = $nearGraduation.output.graduates
    $nearGraduationInvalid = $nearGraduationInvalid -or $nearGraduation.nextState.phase -ne "curve"
    $nearGraduationInvalid = $nearGraduationInvalid -or (To-Amount $nearGraduation.output.ethRefund) -ne 0
    $nearGraduationInvalid = $nearGraduationInvalid -or (
        (To-Amount $nearGraduation.nextState.tokensSold) -ge (To-Amount $artifact.parameters.curveTokens)
    )
    $nearGraduationInvalid = $nearGraduationInvalid -or (
        (To-Amount $nearGraduation.input.ethGross) + 1 -ne (To-Amount $finalExact.input.ethGross)
    )
    if ($nearGraduationInvalid) {
        Fail "buy_just_below_graduation must remain one gross wei below the graduation boundary"
    }
    $fullSell = $artifact.cases | Where-Object id -eq "sell_full"
    if ((To-Amount $fullSell.nextState.tokensSold) -ne 0) {
        Fail "sell_full must restore zero tokens sold"
    }

    if (-not $Write) {
        if (-not (Test-Path -LiteralPath $artifactPath)) {
            Fail "missing vectors/v1/curve-v1.json; run ./scripts/check-vectors.ps1 -Write"
        }
        $expected = [System.IO.File]::ReadAllText($artifactPath)
        $actual = [System.IO.File]::ReadAllText($generatedPath)
        if ($actual -ne $expected) {
            Fail "vectors/v1/curve-v1.json has drifted; review and regenerate it"
        }
        Write-Output "Curve vectors verified."
    }
    else {
        Write-Output "Wrote $artifactPath"
    }

    # ---- v2: generated by executing the real v2 system (factory, curve, hook, locker, v4) ----
    if (-not (Test-Path -LiteralPath $v2SchemaPath)) {
        Fail "missing vectors/v2/curve.schema.json"
    }
    Get-Content -Raw -LiteralPath $v2SchemaPath | ConvertFrom-Json | Out-Null

    [Environment]::SetEnvironmentVariable("CURVE_VECTORS_V2_OUTPUT", $v2OutputPath, "Process")
    & forge script "script/v2/GenerateCurveVectorsV2.s.sol:GenerateCurveVectorsV2" -q
    if ($LASTEXITCODE -ne 0) {
        Fail "v2 Foundry generator exited with $LASTEXITCODE"
    }

    $v2CandidatePath = if ($Write) { $v2ArtifactPath } else { $v2GeneratedPath }
    if (-not (Test-Path -LiteralPath $v2CandidatePath)) {
        Fail "v2 generator did not write $v2CandidatePath"
    }
    # Test-Json ships with PowerShell 7 (CI); Windows PowerShell 5.1 has no schema validator.
    if (Get-Command Test-Json -ErrorAction SilentlyContinue) {
        try {
            $valid = Test-Json -LiteralPath $v2CandidatePath -SchemaFile $v2SchemaPath -ErrorAction Stop
        }
        catch {
            Fail "v2 vectors failed schema validation: $($_.Exception.Message)"
        }
        if (-not $valid) {
            Fail "v2 vectors failed schema validation"
        }
    }
    else {
        Write-Output "Test-Json unavailable; skipped v2 schema validation (requires PowerShell 7)."
    }
    $v2 = Get-Content -Raw -LiteralPath $v2CandidatePath | ConvertFrom-Json

    if ($v2.'$schema' -ne "./curve.schema.json" -or [int] $v2.schemaVersion -ne 1 -or [int] $v2.engineVersion -ne 2) {
        Fail "v2 artifact header has drifted"
    }
    $v2ExpectedIds = @(
        "buy_launch_second",
        "buy_one_wei_launch_second",
        "buy_mid_window",
        "buy_last_window_second",
        "buy_after_window",
        "buy_final_in_window",
        "buy_final_exact_after_window",
        "buy_final_refund_after_window",
        "sell_in_window",
        "invalid_buy_zero_input"
    )
    $v2ActualIds = @($v2.cases | ForEach-Object { [string] $_.id })
    if (($v2ActualIds -join "`n") -ne ($v2ExpectedIds -join "`n")) {
        Fail "v2 case set or deterministic order has drifted"
    }

    $duration = [int] $v2.parameters.antiSnipeDurationSeconds
    $graduationEth = To-Amount $v2.parameters.graduationEth
    $lpTokens = To-Amount $v2.parameters.lpTokens
    foreach ($case in $v2.cases) {
        if ($null -ne $case.expectedRevert) {
            if ($null -ne $case.output -or $null -ne $case.graduation) {
                Fail "$($case.id) has an output or graduation and an expected revert"
            }
            Assert-UnchangedState $case
            continue
        }
        $seconds = [int] $case.secondsSinceLaunch
        $taxBps = [int] $case.output.taxBps
        if (($seconds -ge $duration) -ne ($taxBps -eq 0)) {
            Fail "$($case.id) tax rate disagrees with the anti-snipe window"
        }
        if ($case.operation -eq "buy") {
            $supplied = To-Amount $case.input.ethGross
            $consumed = (To-Amount $case.output.ethGross) + (To-Amount $case.output.ethRefund)
            if ($supplied -ne $consumed) {
                Fail "$($case.id) does not conserve supplied buy ETH"
            }
            $taxDelta = (To-Amount $case.nextState.snipeTax) - (To-Amount $case.initialState.snipeTax)
            if ($taxDelta -ne (To-Amount $case.output.snipeTax)) {
                Fail "$($case.id) snipe tax output differs from the booked tax"
            }
        }
        elseif ((To-Amount $case.output.snipeTax) -ne 0) {
            Fail "$($case.id) taxes a sell"
        }
        if ([bool] $case.output.graduates -ne ($null -ne $case.graduation)) {
            Fail "$($case.id) graduation record disagrees with its output"
        }
        if ($null -ne $case.graduation) {
            $ethConserved = (To-Amount $case.graduation.ethToPool) + (To-Amount $case.graduation.ethRemainder)
            $tokensConserved = (To-Amount $case.graduation.tokensToPool) + (To-Amount $case.graduation.tokenRemainder)
            if ($ethConserved -ne $graduationEth -or $tokensConserved -ne $lpTokens) {
                Fail "$($case.id) graduation does not conserve graduation ETH and LP tokens"
            }
        }
    }
    $graduations = @($v2.cases | Where-Object { $null -ne $_.graduation })
    $prices = @($graduations | ForEach-Object { [string] $_.graduation.sqrtPriceX96 } | Sort-Object -Unique)
    if ($graduations.Count -ne 3 -or $prices.Count -ne 1) {
        Fail "every v2 graduation case must open the pool at the same deterministic price"
    }

    if (-not $Write) {
        if (-not (Test-Path -LiteralPath $v2ArtifactPath)) {
            Fail "missing vectors/v2/curve-v2.json; run ./scripts/check-vectors.ps1 -Write"
        }
        $v2Expected = [System.IO.File]::ReadAllText($v2ArtifactPath)
        $v2Actual = [System.IO.File]::ReadAllText($v2GeneratedPath)
        if ($v2Actual -ne $v2Expected) {
            Fail "vectors/v2/curve-v2.json has drifted; review and regenerate it"
        }
        Write-Output "Curve v2 vectors verified."
    }
    else {
        Write-Output "Wrote $v2ArtifactPath"
    }
}
finally {
    if ($null -eq $previousV2Output) {
        [Environment]::SetEnvironmentVariable("CURVE_VECTORS_V2_OUTPUT", $null, "Process")
    }
    else {
        [Environment]::SetEnvironmentVariable("CURVE_VECTORS_V2_OUTPUT", $previousV2Output, "Process")
    }
    if (Test-Path -LiteralPath $v2GeneratedPath) {
        Remove-Item -LiteralPath $v2GeneratedPath -Force
    }
    if ($null -eq $previousOutput) {
        [Environment]::SetEnvironmentVariable("CURVE_VECTORS_OUTPUT", $null, "Process")
    }
    else {
        [Environment]::SetEnvironmentVariable("CURVE_VECTORS_OUTPUT", $previousOutput, "Process")
    }
    if (Test-Path -LiteralPath $generatedPath) {
        Remove-Item -LiteralPath $generatedPath -Force
    }
    if (Test-Path -LiteralPath $generatedDirectory) {
        Remove-Item -LiteralPath $generatedDirectory -Force
    }
    Pop-Location
}
