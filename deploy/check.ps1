# Local, disposable container verification; never uses production credentials or volumes.
param([switch]$Build)
$ErrorActionPreference = 'Stop'
$repoRoot = Split-Path $PSScriptRoot -Parent
# The newest embedded migration, e.g. 14 for 00014_*.sql; checks must follow new migrations.
$latestMigration = [int](Get-ChildItem (Join-Path $repoRoot 'backend/internal/store/postgres/migrations') -Filter '*.sql' |
    ForEach-Object { [int]($_.Name.Substring(0, 5)) } | Measure-Object -Maximum).Maximum
$suffix = [guid]::NewGuid().ToString('N').Substring(0, 8)
$network = "launchpad-check-$suffix"
$db = "$network-db"
$api = "$network-api"
$web = "$network-web"
$indexer = "$network-indexer"
$prometheus = "$network-prometheus"
$alertmanager = "$network-alertmanager"
$backendImage = 'launchpad-backend:prep-validation'
$webImage = 'launchpad-web:prep-validation'
$prometheusImage = 'prom/prometheus:v3.15.1@sha256:fa3302279710be2f21a8301ec17a8b4f2301fd829bd113839b3b6b02b6f191a2'
$alertmanagerImage = 'prom/alertmanager:v0.34.1@sha256:e9733bafb1bdef9b00e25a21f8f99dc26a22224bf16641ad754d1649f4c3357a'
$webhookExample = [IO.Path]::GetFullPath("$PSScriptRoot/monitoring/webhook-url.example")

# A real webhook URL is a secret and must live outside the repository; only the placeholder
# example (a receiver that cannot reach anything) may be referenced from inside it.
function Assert-WebhookSecretPath {
    param([string]$Path)
    if (-not $Path) { return }
    $full = [IO.Path]::GetFullPath($Path)
    $root = [IO.Path]::GetFullPath($repoRoot).TrimEnd('\', '/') + [IO.Path]::DirectorySeparatorChar
    $insideRepo = $full.StartsWith($root, [StringComparison]::OrdinalIgnoreCase)
    if ($insideRepo -and -not $full.Equals($webhookExample, [StringComparison]::OrdinalIgnoreCase)) {
        throw 'ALERTMANAGER_WEBHOOK_URL_FILE must be outside the repository (only monitoring/webhook-url.example is allowed inside)'
    }
}

function Test-WebhookSecretGuard {
    Assert-WebhookSecretPath $env:ALERTMANAGER_WEBHOOK_URL_FILE
    Assert-WebhookSecretPath $webhookExample
    Assert-WebhookSecretPath (Join-Path ([IO.Path]::GetTempPath()) 'alertmanager-webhook-url')
    $rejected = $false
    try { Assert-WebhookSecretPath (Join-Path $PSScriptRoot 'monitoring/webhook-url') } catch { $rejected = $true }
    if (-not $rejected) { throw 'Webhook secret guard accepted a path inside the repository' }
}

function Invoke-Docker {
    param([string[]]$Arguments)
    $output = & docker @Arguments
    if ($LASTEXITCODE -ne 0) { throw "Docker command failed: $($Arguments[0])" }
    return $output
}

# Resolves compose.yaml with placeholder public values (never real secrets); Compose fails on a
# malformed file or a required variable without a value. Needs no running Docker daemon.
function Test-ComposeConfig {
    param([string]$SecretFile)
    $values = [ordered]@{
        RELEASE_TAG = 'prep-validation'
        BACKEND_ENV_FILE = "$PSScriptRoot/backend.env.example"
        POSTGRES_PASSWORD_FILE = $SecretFile
        NEXT_PUBLIC_CHAIN_ID = '46630'
        NEXT_PUBLIC_DEPLOYMENT_ID = 'compose-validation'
        NEXT_PUBLIC_PRIVY_APP_ID = 'compose-validation'
        NEXT_PUBLIC_RPC_URL = 'https://rpc.example.invalid'
        WEB_DOMAIN = 'web.example.invalid'
        API_DOMAIN = 'api.example.invalid'
        ACME_EMAIL = 'ops@example.invalid'
    }
    $envFile = "$SecretFile.env"
    try {
        [IO.File]::WriteAllLines($envFile, [string[]]($values.Keys | ForEach-Object { "$_=$($values[$_])" }))
        Invoke-Docker @('compose', '-f', "$PSScriptRoot/compose.yaml", '--env-file', $envFile, 'config', '--quiet') | Out-Null
        Invoke-Docker @('compose', '-f', "$PSScriptRoot/compose.yaml", '--env-file', $envFile,
            '--profile', 'monitoring', 'config', '--quiet') | Out-Null
        $defaultServices = Invoke-Docker @('compose', '-f', "$PSScriptRoot/compose.yaml", '--env-file', $envFile, 'config', '--services')
        if (($defaultServices -join "`n") -match 'prometheus|alertmanager') { throw 'Monitoring services must be opt-in (profile monitoring)' }
        [IO.File]::WriteAllLines($envFile, [string[]]($values.Keys | Where-Object { $_ -ne 'WEB_DOMAIN' } |
            ForEach-Object { "$_=$($values[$_])" }))
        & docker compose -f "$PSScriptRoot/compose.yaml" --env-file $envFile config --quiet 2>$null | Out-Null
        if ($LASTEXITCODE -eq 0) { throw 'Compose accepted a configuration without WEB_DOMAIN' }
    } finally {
        Remove-Item -LiteralPath $envFile -Force -ErrorAction SilentlyContinue
    }
}

$composeSecret = New-TemporaryFile
try {
    Test-WebhookSecretGuard
    Test-ComposeConfig -SecretFile $composeSecret.FullName
    if ($Build) {
        Invoke-Docker @('build', '-t', $backendImage, "$repoRoot/backend")
        Invoke-Docker @('build', '-t', $webImage, "$repoRoot/web")
    }
    Invoke-Docker @('network', 'create', $network) | Out-Null
    Invoke-Docker @('run', '-d', '--name', $db, '--network', $network,
        '-e', 'POSTGRES_USER=launchpad', '-e', 'POSTGRES_DB=launchpad',
        '-e', 'POSTGRES_PASSWORD=disposable-smoke-only', 'postgres:18.6-alpine') | Out-Null
    $ready = $false
    for ($attempt = 0; $attempt -lt 30; $attempt++) {
        & docker exec $db pg_isready -h 127.0.0.1 -U launchpad -d launchpad 2>$null | Out-Null
        if ($LASTEXITCODE -eq 0) { $ready = $true; break }
        Start-Sleep -Seconds 1
    }
    if (-not $ready) { throw 'Disposable PostgreSQL did not become ready' }
    $databaseUrl = "DATABASE_URL=postgres://launchpad:disposable-smoke-only@${db}:5432/launchpad?sslmode=disable"
    Invoke-Docker @('run', '--rm', '--network', $network, '-e', $databaseUrl,
        $backendImage, '/app/migrate', 'up') | Out-Null
    $migrationStatus = Invoke-Docker @('run', '--rm', '--network', $network, '-e', $databaseUrl,
        $backendImage, '/app/migrate', 'status')
    if (($migrationStatus -join "`n") -notmatch ('{0:D5}\s+applied' -f $latestMigration)) { throw 'Latest migration is not applied' }
    Invoke-Docker @('exec', $db, 'pg_dump', '-U', 'launchpad', '-d', 'launchpad', '-Fc', '-f', '/tmp/launchpad.dump') | Out-Null
    Invoke-Docker @('exec', $db, 'createdb', '-U', 'launchpad', 'launchpad_restore') | Out-Null
    Invoke-Docker @('exec', $db, 'pg_restore', '--exit-on-error', '-U', 'launchpad', '-d', 'launchpad_restore', '/tmp/launchpad.dump') | Out-Null
    $restoredVersion = Invoke-Docker @('exec', $db, 'psql', '-U', 'launchpad', '-d', 'launchpad_restore', '-Atc',
        'SELECT MAX(version_id) FROM goose_db_version WHERE is_applied;')
    if (($restoredVersion -join '').Trim() -ne [string]$latestMigration) { throw 'Backup restore did not preserve migration version' }

    $verificationKey = [Security.Cryptography.ECDsa]::Create([Security.Cryptography.ECCurve+NamedCurves]::nistP256)
    try { $publicPem = $verificationKey.ExportSubjectPublicKeyInfoPem() } finally { $verificationKey.Dispose() }
    $chainEnv = @('-e', $databaseUrl, '-e', 'CHAIN_ID=46630', '-e', 'DEPLOYMENT_ID=robinhood-testnet-v1',
        '-e', 'RPC_URL=https://rpc.testnet.chain.robinhood.com')
    # The metrics port is never published; only Prometheus on the check network reaches it.
    Invoke-Docker (@('run', '-d', '--name', $api, '--network', $network, '--network-alias', 'api',
        '--read-only', '--tmpfs', '/tmp', '--cap-drop', 'ALL', '--security-opt', 'no-new-privileges:true',
        '-p', '127.0.0.1::8080') + $chainEnv + @('-e', 'API_METRICS_ADDR=:8082',
        '-e', 'PRIVY_APP_ID=container-check-only', '-e', "PRIVY_VERIFICATION_KEY=$publicPem",
        '-e', 'API_ALLOWED_ORIGINS=https://web.example.invalid', $backendImage)) | Out-Null
    Invoke-Docker @('run', '-d', '--name', $web, '--read-only', '--tmpfs', '/tmp',
        '--tmpfs', '/app/.next/cache:uid=1000,gid=1000', '--cap-drop', 'ALL',
        '--security-opt', 'no-new-privileges:true', '-p', '127.0.0.1::3000', $webImage) | Out-Null
    $apiBinding = (Invoke-Docker @('port', $api, '8080/tcp')).Trim()
    $webBinding = (Invoke-Docker @('port', $web, '3000/tcp')).Trim()
    $apiHealthy = $false
    for ($attempt = 0; $attempt -lt 20; $attempt++) {
        try {
            $health = Invoke-RestMethod "http://$apiBinding/healthz"
            $html = Invoke-WebRequest "http://$webBinding/"
            if ($health.status -eq 'ok' -and $html.StatusCode -eq 200) { $apiHealthy = $true; break }
        } catch { Start-Sleep -Seconds 1 }
    }
    if (-not $apiHealthy) { throw 'Container HTTP smoke check failed' }
    $asset = [regex]::Match($html.Content, 'src="([^" ]+/_next/[^" ]+|/_next/[^" ]+)"').Groups[1].Value
    if (-not $asset) { throw 'Web HTML did not reference a compiled asset' }
    if ((Invoke-WebRequest "http://$webBinding$asset").StatusCode -ne 200) { throw 'Compiled asset is missing' }
    if (-not $html.Headers['Content-Security-Policy']) { throw 'Web CSP header is missing' }
    $readyResponse = Invoke-WebRequest "http://$apiBinding/readyz" -SkipHttpErrorCheck
    if ($readyResponse.StatusCode -ne 503) { throw 'Unindexed database must not be ready' }
    # Caddy only forwards to api:8080, so a 404 here proves the public listener serves no metrics.
    $publicMetrics = Invoke-WebRequest "http://$apiBinding/metrics" -SkipHttpErrorCheck
    if ($publicMetrics.StatusCode -ne 404) { throw 'Public API listener must not serve /metrics' }
    # Started only after the readiness rejection: the indexer would commit a watermark and make /readyz 200.
    Invoke-Docker (@('run', '-d', '--name', $indexer, '--network', $network, '--network-alias', 'indexer',
        '--read-only', '--tmpfs', '/tmp', '--cap-drop', 'ALL', '--security-opt', 'no-new-privileges:true') + $chainEnv +
        @('-e', 'INDEXER_HEALTH_ADDR=:8081', '-e', 'INDEXER_WORKER_ID=disposable-check',
        $backendImage, '/app/indexer')) | Out-Null

    Invoke-Docker @('run', '-d', '--name', $alertmanager, '--network', $network, '--network-alias', 'alertmanager',
        '--read-only', '--tmpfs', '/alertmanager:uid=65534,gid=65534', '--cap-drop', 'ALL',
        '--security-opt', 'no-new-privileges:true',
        '-v', "${PSScriptRoot}/monitoring/alertmanager.yml:/etc/alertmanager/alertmanager.yml:ro",
        '-v', "${webhookExample}:/run/secrets/alertmanager_webhook_url:ro",
        $alertmanagerImage, '--config.file=/etc/alertmanager/alertmanager.yml', '--storage.path=/alertmanager') | Out-Null
    Invoke-Docker @('run', '-d', '--name', $prometheus, '--network', $network,
        '--read-only', '--tmpfs', '/prometheus:uid=65534,gid=65534', '--cap-drop', 'ALL',
        '--security-opt', 'no-new-privileges:true', '-p', '127.0.0.1::9090',
        '-v', "${PSScriptRoot}/monitoring/prometheus.yml:/etc/prometheus/prometheus.yml:ro",
        '-v', "${PSScriptRoot}/monitoring/alerts.yml:/etc/prometheus/alerts.yml:ro",
        $prometheusImage, '--config.file=/etc/prometheus/prometheus.yml', '--storage.tsdb.path=/prometheus') | Out-Null
    $prometheusBinding = (Invoke-Docker @('port', $prometheus, '9090/tcp') | Select-Object -First 1).Trim()
    $targetsUp = $false
    for ($attempt = 0; $attempt -lt 60 -and -not $targetsUp; $attempt++) {
        try {
            $targets = (Invoke-RestMethod "http://$prometheusBinding/api/v1/targets").data.activeTargets
            $upJobs = @($targets | Where-Object { $_.health -eq 'up' } | ForEach-Object { $_.labels.job })
            $targetsUp = ($upJobs -contains 'api') -and ($upJobs -contains 'indexer')
        } catch { }
        if (-not $targetsUp) { Start-Sleep -Seconds 2 }
    }
    if (-not $targetsUp) { throw 'Prometheus did not report both api and indexer targets up' }

    & docker run --rm $webImage node scripts/container-preflight.mjs 2>$null | Out-Null
    if ($LASTEXITCODE -eq 0) { throw 'Unconfigured validation image was accepted for production' }
    Invoke-Docker @('run', '--rm', '-e', 'WEB_DOMAIN=web.example.invalid',
        '-e', 'API_DOMAIN=api.example.invalid', '-e', 'ACME_EMAIL=ops@example.invalid',
        '-v', "${PSScriptRoot}/Caddyfile:/etc/caddy/Caddyfile:ro", 'caddy:2.10.2-alpine',
        'caddy', 'validate', '--config', '/etc/caddy/Caddyfile', '--adapter', 'caddyfile') | Out-Null
    Write-Output 'PASS: migrations, backup/restore, non-root read-only API/web, HTML/assets/CSP, readiness rejection, production preflight rejection, Compose configuration (including the monitoring profile), Caddy validation, no public /metrics, Prometheus scraping api and indexer.'
} finally {
    # Only random-name resources created by this check are removed; no production volume is touched.
    & docker rm -f -v $api $web $db $indexer $prometheus $alertmanager 2>$null | Out-Null
    & docker network rm $network 2>$null | Out-Null
    Remove-Item -LiteralPath $composeSecret.FullName -Force -ErrorAction SilentlyContinue
}
