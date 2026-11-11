# Starts a persistent local demo stack: Anvil, PostgreSQL (Docker), the API, the indexer, seeded
# launches, and the Next dev server on http://127.0.0.1:3417. Runs until Ctrl+C, then tears
# everything down. Local only: Anvil's well-known dev accounts, no testnet/mainnet transaction.
# Deployment and runtime wiring mirror scripts/anvil-task6-gate.ps1.
param(
    [int]$AnvilPort = 8545,
    [int]$PostgresPort = 55432,
    [int]$ApiPort = 8080,
    [int]$IndexerHealthPort = 8081,
    [int]$WebPort = 3417,
    [switch]$NoSeed
)

$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest

$webRoot = Split-Path -Parent $PSScriptRoot
$repoRoot = Split-Path -Parent $webRoot
$backendRoot = Join-Path $repoRoot "backend"
$contractsRoot = Join-Path $repoRoot "contracts"
$manifestPath = Join-Path $contractsRoot "deployments/.generated/local-stack.json"
$deploymentId = "task6-anvil"
# Anvil default accounts 0-5 (public, deterministic dev keys; never funded anywhere real).
$accounts = @(
    "0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266",
    "0x70997970C51812dc3A010C7d01b50e0d17dc79C8",
    "0x3C44CdDdB6a900fa2b585dd299e03d12FA4293BC",
    "0x90F79bf6EB2c4f870365E785982E1f101E93b906",
    "0x15d34AAf54267DB7D7c367839AAf71A00a2C6A65",
    "0x9965507D1a55bcC2695C58ba16FB37d819B0A4dc"
)
$sender = $accounts[0]
$postgresImage = "postgres:18.6-alpine"
$postgresContainer = "launchpad-local-postgres"
$databaseName = "launchpad_local"
$runningOnWindows = [System.Environment]::OSVersion.Platform -eq [System.PlatformID]::Win32NT
$powershellCommand = if ($runningOnWindows) { "powershell.exe" } else { "pwsh" }
$npmCommand = if ($runningOnWindows) { "npm.cmd" } else { "npm" }
$foundryExtension = if ($runningOnWindows) { ".exe" } else { "" }
$logRoot = Join-Path $backendRoot ".cache/local-stack"
$children = New-Object System.Collections.ArrayList
$postgresStarted = $false

function Invoke-Checked([string]$FilePath, [string[]]$Arguments) {
    & $FilePath @Arguments
    if ($LASTEXITCODE -ne 0) { throw "$FilePath failed with exit code $LASTEXITCODE" }
}
function Assert-PortFree([int]$Port) {
    $listener = [System.Net.Sockets.TcpListener]::new([System.Net.IPAddress]::Loopback, $Port)
    try { $listener.Start() } catch { throw "Port $Port is already in use; stop that process or pass another port" }
    finally { try { $listener.Stop() } catch { } }
}
function Start-Child([string]$FilePath, [string]$Arguments, [string]$WorkingDirectory, [hashtable]$Environment, [string]$Label) {
    $stdoutLog = Join-Path $logRoot "$Label.stdout.log"
    $stderrLog = Join-Path $logRoot "$Label.stderr.log"
    foreach ($entry in $Environment.GetEnumerator()) { Set-Item -Path ("Env:" + $entry.Key) -Value ([string]$entry.Value) }
    $start = @{ FilePath = $FilePath; WorkingDirectory = $WorkingDirectory; PassThru = $true; RedirectStandardOutput = $stdoutLog; RedirectStandardError = $stderrLog }
    if ($runningOnWindows) { $start.WindowStyle = "Hidden" }
    if (-not [string]::IsNullOrWhiteSpace($Arguments)) { $start.ArgumentList = $Arguments }
    $child = Start-Process @start
    if ($null -eq $child) { throw "Could not start $FilePath" }
    [void]$children.Add($child)
    return $child
}
function Stop-Tree($child) {
    if ($null -eq $child) { return }
    try {
        if (-not $child.HasExited) {
            if ($runningOnWindows) { & taskkill.exe /PID $child.Id /T /F 2>$null | Out-Null } else { $child.Kill() }
            $child.WaitForExit(5000) | Out-Null
        }
    } catch { }
}
function Wait-Http([string]$Url, [int]$Attempts = 240) {
    for ($attempt = 0; $attempt -lt $Attempts; $attempt++) {
        try {
            $response = Invoke-WebRequest -UseBasicParsing -Uri $Url -TimeoutSec 5
            if ([int]$response.StatusCode -eq 200) { return }
        } catch { }
        Start-Sleep -Milliseconds 500
    }
    throw "Timed out waiting for $Url"
}
function Wait-Postgres {
    for ($attempt = 0; $attempt -lt 120; $attempt++) {
        & docker exec $postgresContainer pg_isready -h 127.0.0.1 -U postgres -d postgres 2>$null | Out-Null
        if ($LASTEXITCODE -eq 0) {
            & docker exec -e PGPASSWORD=postgres $postgresContainer psql -h 127.0.0.1 -v ON_ERROR_STOP=1 -U postgres -d postgres -c "SELECT 1" 2>$null | Out-Null
            if ($LASTEXITCODE -eq 0) { return }
        }
        Start-Sleep -Milliseconds 250
    }
    throw "PostgreSQL did not become ready"
}
function Cast([string[]]$Arguments) {
    # cast writes warnings to stderr; judge it by exit code only.
    $ErrorActionPreference = "Continue"
    $output = & $castPath @Arguments 2>&1 | Out-String
    if ($LASTEXITCODE -ne 0) { throw "cast $($Arguments[0]) failed: $output" }
    return $output.Trim()
}
function Deadline { return [string]([DateTimeOffset]::UtcNow.ToUnixTimeSeconds() + 3600) }

# Launches a token and returns its curve address. The return value is read with a static call
# first; the launch then lands in the next block from the same factory nonce, so it matches.
function New-Launch([string]$From, [string]$Name, [string]$Symbol, [decimal]$DevBuyEth) {
    $devBuy = Cast @("to-wei", [string]$DevBuyEth)
    $fee = Cast @("call", $factory, "launchFee()(uint256)", "--rpc-url", $rpcUrl)
    $fee = ($fee -split "\s")[0]
    $value = [System.Numerics.BigInteger]::Parse($fee) + [System.Numerics.BigInteger]::Parse($devBuy)
    $request = "($Name,$Symbol,1,$devBuy,0,$(Deadline))"
    $signature = "launch((string,string,uint16,uint256,uint256,uint256))(address,address,address)"
    $returned = Cast @("call", $factory, $signature, $request, "--value", [string]$value, "--from", $From, "--rpc-url", $rpcUrl)
    $curve = ($returned -split "\s+")[1]
    if ($curve -notmatch "^0x[0-9a-fA-F]{40}$") { throw "Could not read the curve address for $Symbol" }
    Cast @("send", $factory, "launch((string,string,uint16,uint256,uint256,uint256))", $request, "--value", [string]$value, "--from", $From, "--unlocked", "--rpc-url", $rpcUrl) | Out-Null
    return $curve
}
function Buy([string]$Curve, [string]$From, [decimal]$Eth) {
    $wei = Cast @("to-wei", [string]$Eth)
    Cast @("send", $Curve, "buy(address,address,uint256,uint256)", $From, $From, "0", (Deadline), "--value", $wei, "--from", $From, "--unlocked", "--rpc-url", $rpcUrl) | Out-Null
}

foreach ($command in @("docker", "go", $npmCommand, $powershellCommand)) {
    if ($null -eq (Get-Command $command -ErrorAction SilentlyContinue)) { throw "$command is required for the local stack" }
}
$foundryBin = Join-Path ([Environment]::GetFolderPath([Environment+SpecialFolder]::UserProfile)) ".foundry/bin"
function Find-Foundry([string]$Name) {
    $found = Get-Command $Name -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1
    $path = if ($found) { $found.Source } else { Join-Path $foundryBin "$Name$foundryExtension" }
    if (-not (Test-Path -LiteralPath $path)) { throw "Foundry executable not found: $path" }
    return $path
}
$anvilPath = Find-Foundry "anvil"
$castPath = Find-Foundry "cast"
$env:Path = "$(Split-Path -Parent $anvilPath)$([IO.Path]::PathSeparator)$env:Path"
# A previous run stopped without its cleanup leaves this script's own container holding the port.
& docker rm --force $postgresContainer 2>$null | Out-Null
foreach ($port in @($AnvilPort, $PostgresPort, $ApiPort, $IndexerHealthPort, $WebPort)) { Assert-PortFree $port }

$rpcUrl = "http://127.0.0.1:$AnvilPort"
$databaseUrl = "postgres://postgres:postgres@127.0.0.1:$PostgresPort/${databaseName}?sslmode=disable"
$apiUrl = "http://127.0.0.1:$ApiPort"
$webUrl = "http://127.0.0.1:$WebPort"
New-Item -ItemType Directory -Path $logRoot -Force | Out-Null
$env:GOCACHE = Join-Path $backendRoot ".cache/task6-go-build"

try {
    if (Test-Path -LiteralPath $manifestPath) { Remove-Item -LiteralPath $manifestPath -Force }
    & docker rm --force $postgresContainer 2>$null | Out-Null
    Write-Host "Starting PostgreSQL..."
    Invoke-Checked "docker" @("run", "--name", $postgresContainer, "--detach", "--publish", "127.0.0.1:$PostgresPort`:5432", "--env", "POSTGRES_USER=postgres", "--env", "POSTGRES_PASSWORD=postgres", "--env", "POSTGRES_DB=postgres", $postgresImage) | Out-Null
    $postgresStarted = $true
    Wait-Postgres
    Invoke-Checked "docker" @("exec", $postgresContainer, "psql", "-v", "ON_ERROR_STOP=1", "-U", "postgres", "-d", "postgres", "-c", "CREATE DATABASE $databaseName") | Out-Null

    Write-Host "Starting Anvil..."
    Start-Child $anvilPath "--host 127.0.0.1 --port $AnvilPort --chain-id 31337 --silent" $repoRoot @{} "anvil" | Out-Null
    for ($attempt = 0; $attempt -lt 120; $attempt++) {
        $chain = (& $castPath chain-id --rpc-url $rpcUrl 2>$null | Out-String).Trim()
        if ($LASTEXITCODE -eq 0 -and $chain -eq "31337") { break }
        Start-Sleep -Milliseconds 200
        if ($attempt -eq 119) { throw "Anvil RPC did not become ready" }
    }

    Write-Host "Deploying contracts..."
    Invoke-Checked $powershellCommand @(
        "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", (Join-Path $contractsRoot "scripts/deploy.ps1"),
        "-Target", "anvil", "-RpcUrl", $rpcUrl, "-DeploymentId", $deploymentId,
        "-Sender", $sender, "-PauseAuthority", $accounts[1], "-Timelock", $accounts[2],
        "-ProtocolTreasury", $accounts[3], "-Broadcast", "-Unlocked", "-OutputPath", $manifestPath
    ) | Out-Null
    $manifest = Get-Content -Raw -LiteralPath $manifestPath | ConvertFrom-Json
    $factory = [string]$manifest.factory
    $weth = [string]$manifest.weth
    $uniswapFactory = [string]$manifest.uniswapV2Factory
    $router = [string]$manifest.uniswapV2Router02
    foreach ($address in @($factory, $weth, $uniswapFactory, $router)) {
        if ($address -notmatch "^0x[0-9a-fA-F]{40}$") { throw "Deployment manifest has an invalid address: $address" }
    }

    Write-Host "Migrating the database..."
    $env:DATABASE_URL = $databaseUrl
    Push-Location $backendRoot
    try {
        Invoke-Checked "go" @("run", "./cmd/migrate", "up") | Out-Null
        Invoke-Checked "go" @("build", "-o", (Join-Path $logRoot "api.exe"), "./cmd/api")
        Invoke-Checked "go" @("build", "-o", (Join-Path $logRoot "indexer.exe"), "./cmd/indexer")
    } finally { Pop-Location }

    $runtimeEnv = @{
        CHAIN_ID = "31337"; DEPLOYMENT_ID = $deploymentId; RPC_URL = $rpcUrl; DATABASE_URL = $databaseUrl
        API_ADDR = "127.0.0.1:$ApiPort"; API_ALLOWED_ORIGINS = $webUrl; INDEXER_HEALTH_ADDR = "127.0.0.1:$IndexerHealthPort"
        INDEXER_CHUNK_SIZE = "100"; INDEXER_LOG_ADDRESS_BATCH_SIZE = "500"; INDEXER_POLL_INTERVAL = "500ms"
        RPC_TIMEOUT = "2s"; RPC_MAX_RETRIES = "2"; RPC_RETRY_BACKOFF = "50ms"; INDEXER_WORKER_ID = "local-indexer"
        INDEXER_CONFIRMATIONS = "0"; PRIVY_APP_ID = "task6-anvil"
        # Test-only public key from the Anvil gate; no matching private key is used here.
        PRIVY_VERIFICATION_KEY = "-----BEGIN PUBLIC KEY-----`nMFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAEtOOEaFeGAEoHOSkD0owE7VmvwjcI`n3lpjzKdfLSJaQvLUZnKvBdO3mWLsvulsCuB0PJCgycRbVvqZqyhN1txH4A==`n-----END PUBLIC KEY-----`n"
        DEPLOYMENT_MANIFEST_PATH = $manifestPath
    }
    Write-Host "Starting the API and indexer..."
    Start-Child (Join-Path $logRoot "api.exe") "" $backendRoot $runtimeEnv "api" | Out-Null
    Wait-Http "$apiUrl/healthz"
    Start-Child (Join-Path $logRoot "indexer.exe") "" $backendRoot $runtimeEnv "indexer" | Out-Null
    Wait-Http "http://127.0.0.1:$IndexerHealthPort/healthz"

    if (-not $NoSeed) {
        Write-Host "Seeding launches and trades..."
        $launches = @(
            @{ Name = "Orbit Cat"; Symbol = "ORBIT"; Dev = 0.01; Buys = @(0.12, 0.08, 0.25, 0.04) },
            @{ Name = "Paper Hands"; Symbol = "PAPER"; Dev = 0.005; Buys = @(0.03, 0.06) },
            @{ Name = "Night Market"; Symbol = "NIGHT"; Dev = 0.01; Buys = @(0.15, 0.3, 0.07, 0.22, 0.11) },
            @{ Name = "Signal Fire"; Symbol = "FIRE"; Dev = 0.002; Buys = @(0.025) },
            @{ Name = "Quiet Harbor"; Symbol = "HRBR"; Dev = 0; Buys = @(0.09, 0.035, 0.14) },
            @{ Name = "Moon Ledger"; Symbol = "MOON"; Dev = 0.01; Buys = @(0.4, 0.25, 0.33); Graduate = $true },
            @{ Name = "Copper Fox"; Symbol = "FOX"; Dev = 0.01; Buys = @(0.3, 0.5, 0.2); Graduate = $true },
            @{ Name = "Glass Tiger"; Symbol = "GLASS"; Dev = 0.005; Buys = @(0.6, 0.15); Graduate = $true },
            @{ Name = "Velvet Rocket"; Symbol = "VLVT"; Dev = 0.01; Buys = @(0.2, 0.45, 0.1, 0.35); Graduate = $true },
            @{ Name = "Solar Drift"; Symbol = "SOLAR"; Dev = 0; Buys = @(0.5, 0.25); Graduate = $true },
            @{ Name = "Iron Lotus"; Symbol = "LOTUS"; Dev = 0.008; Buys = @(0.15, 0.3, 0.4); Graduate = $true },
            @{ Name = "Pixel Whale"; Symbol = "WHALE"; Dev = 0.01; Buys = @(0.7, 0.2, 0.12); Graduate = $true },
            @{ Name = "Static Bloom"; Symbol = "BLOOM"; Dev = 0.004; Buys = @(0.25, 0.18); Graduate = $true },
            @{ Name = "Amber Comet"; Symbol = "AMBER"; Dev = 0.006; Buys = @(0.3, 0.22); Graduate = $true },
            @{ Name = "Coral Signal"; Symbol = "CORAL"; Dev = 0; Buys = @(0.18, 0.4, 0.09); Graduate = $true }
        )
        $index = 0
        $activeCurves = New-Object System.Collections.ArrayList
        foreach ($launch in $launches) {
            $creator = $accounts[$index % $accounts.Count]
            $curve = New-Launch $creator $launch.Name $launch.Symbol ([decimal]$launch.Dev)
            $traderIndex = $index
            foreach ($eth in $launch.Buys) {
                $traderIndex++
                try { Buy $curve $accounts[$traderIndex % $accounts.Count] ([decimal]$eth) }
                catch { Write-Warning "$($launch.Symbol) buy skipped: $($_.Exception.Message)" }
            }
            # Graduating launches are bought past their threshold so the Graduated lists fill;
            # the curve refunds any excess.
            if ($launch.ContainsKey("Graduate")) {
                try { Buy $curve $accounts[($index + 4) % $accounts.Count] ([decimal]200); Write-Host "  $($launch.Symbol) -> $curve (graduated)" }
                catch { Write-Warning "$($launch.Symbol) graduation buy did not land: $($_.Exception.Message)" }
            } else {
                Write-Host "  $($launch.Symbol) -> $curve"
                [void]$activeCurves.Add($curve)
            }
            $index++
        }
    }

    if (-not $NoSeed) {
        # The indexer has to have projected the launches before comments can reference them.
        for ($attempt = 0; $attempt -lt 60; $attempt++) {
            $count = (& docker exec $postgresContainer psql -U postgres -d $databaseName -Atc "SELECT count(*) FROM tokens" | Out-String).Trim()
            if ($count -eq [string]$launches.Count) { break }
            Start-Sleep -Milliseconds 500
        }
        Write-Host "Seeding forum posts..."
        Get-Content -Raw -LiteralPath (Join-Path $PSScriptRoot "local-stack-forum.sql") |
            & docker exec -i $postgresContainer psql -v ON_ERROR_STOP=1 -U postgres -d $databaseName | Out-Null
        if ($LASTEXITCODE -ne 0) { throw "Forum seed failed" }
        Write-Host "Seeding token artwork..."
        $addresses = @(& docker exec $postgresContainer psql -U postgres -d $databaseName -Atc "SELECT '0x' || encode(token_address, 'hex') FROM tokens")
        & node (Join-Path $PSScriptRoot "local-stack-images.mjs") @addresses |
            & docker exec -i $postgresContainer psql -v ON_ERROR_STOP=1 -U postgres -d $databaseName | Out-Null
        if ($LASTEXITCODE -ne 0) { throw "Artwork seed failed" }
    }

    Write-Host "Starting the web app..."
    $webEnv = @{
        NEXT_PUBLIC_E2E_FIXTURE = "1"; NEXT_PUBLIC_LOCAL_DEV = "1"
        NEXT_PUBLIC_TASK6_ANVIL_FACTORY = $factory; NEXT_PUBLIC_TASK6_ANVIL_WETH = $weth
        NEXT_PUBLIC_TASK6_ANVIL_ROUTER = $router; NEXT_PUBLIC_TASK6_ANVIL_UNISWAP_FACTORY = $uniswapFactory
        NEXT_PUBLIC_TASK6_ANVIL_RPC_URL = $rpcUrl; NEXT_PUBLIC_TASK6_ANVIL_API_URL = $apiUrl
        NEXT_PUBLIC_TASK6_ANVIL_WEB_URL = $webUrl; NEXT_PUBLIC_WEB_ORIGIN = $webUrl
        TASK6_ANVIL_RPC_URL = $rpcUrl; API_INTERNAL_BASE_URL = $apiUrl
    }
    $web = Start-Child $npmCommand "run dev -- --hostname 127.0.0.1 --port $WebPort" $webRoot $webEnv "web"
    Wait-Http $webUrl
    Write-Host ""
    Write-Host "Local stack is up: $webUrl  (API $apiUrl, RPC $rpcUrl). Logs: $logRoot"
    Write-Host "Press Ctrl+C to stop."
    # Keep the feed alive: a small buy from a random dev account every ~20 seconds, so Trending
    # (a recent-activity window) stays populated and the live strip keeps receiving trades.
    $random = [System.Random]::new()
    $tick = 0
    while ($true) {
        foreach ($child in $children) {
            if ($child.HasExited) { throw "A stack process exited (code $($child.ExitCode)); see $logRoot" }
        }
        $tick++
        if (-not $NoSeed -and $activeCurves.Count -gt 0 -and $tick % 10 -eq 0) {
            $curve = $activeCurves[$random.Next($activeCurves.Count)]
            $trader = $accounts[1 + $random.Next($accounts.Count - 1)]
            $eth = [decimal]::Round([decimal]($random.Next(5, 80)) / 1000, 3)
            try { Buy $curve $trader $eth }
            catch { Write-Warning "Background buy skipped: $($_.Exception.Message)" }
        }
        Start-Sleep -Seconds 2
    }
}
finally {
    Write-Host "Stopping the local stack..."
    for ($i = $children.Count - 1; $i -ge 0; $i--) { Stop-Tree $children[$i] }
    if ($postgresStarted) { & docker rm --force $postgresContainer 2>$null | Out-Null }
    if (Test-Path -LiteralPath $manifestPath) { Remove-Item -LiteralPath $manifestPath -Force }
}
