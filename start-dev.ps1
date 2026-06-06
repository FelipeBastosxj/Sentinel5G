# start-dev.ps1  — Inicia os 3 serviços NestJS em janelas PowerShell separadas
# Uso:  .\start-dev.ps1
param(
    [string]$DatabaseUrl = "postgresql://webhook_user:webhook_pass@localhost:5432/telecom_webhooks"
)

$BASE = "C:\Users\Felipe\Desktop\eventstream-observability-engine"

# ── Carregar .env se existir ──────────────────────────────────────────────────
$envFile = Join-Path $BASE ".env"
if (Test-Path $envFile) {
    Get-Content $envFile | Where-Object { $_ -match '^\s*[^#\s]' } | ForEach-Object {
        $parts = $_ -split '=', 2
        if ($parts.Count -eq 2) {
            $name  = $parts[0].Trim()
            $value = $parts[1].Trim().Trim('"').Trim("'")
            [System.Environment]::SetEnvironmentVariable($name, $value, 'Process')
        }
    }
    Write-Host "[start-dev] .env carregado" -ForegroundColor Cyan
}

# Garantir variáveis essenciais
if (-not $env:DATABASE_URL) { $env:DATABASE_URL = $DatabaseUrl }
if (-not $env:NODE_ENV)     { $env:NODE_ENV = "development" }

Write-Host "[start-dev] DATABASE_URL = $env:DATABASE_URL" -ForegroundColor DarkGray
Write-Host "[start-dev] NODE        = $(node --version 2>&1)" -ForegroundColor DarkGray

# ── Matar processos antigos nas portas ────────────────────────────────────────
Write-Host "`n[start-dev] Liberando portas 3001, 3003, 3004..." -ForegroundColor Yellow
foreach ($port in @(3001, 3003, 3004)) {
    $conns = Get-NetTCPConnection -LocalPort $port -ErrorAction SilentlyContinue
    foreach ($conn in $conns) {
        $pid = $conn.OwningProcess
        if ($pid -and $pid -ne 0) {
            try { Stop-Process -Id $pid -Force -ErrorAction Stop; Write-Host "  Encerrou PID $pid (porta $port)" } catch {}
        }
    }
}
Start-Sleep -Seconds 1

# ── Função auxiliar: abre serviço em nova janela PowerShell ───────────────────
function Start-Service {
    param([string]$Label, [string]$Workspace, [hashtable]$Env)

    $envLines = ($Env.GetEnumerator() | ForEach-Object {
        "`$env:$($_.Key)='$($_.Value)'"
    }) -join "; "

    $cmd = "$envLines; cd '$BASE'; npm run start:dev --workspace=$Workspace"

    Write-Host "[start-dev] Iniciando $Label..." -ForegroundColor Green
    Start-Process powershell -ArgumentList "-NoExit", "-Command", $cmd -WindowStyle Normal
}

# ── Iniciar os 3 serviços ─────────────────────────────────────────────────────
Start-Service -Label "processing-service (3003)" -Workspace "services/processing-service" -Env @{
    DATABASE_URL    = $env:DATABASE_URL
    PROCESSING_PORT = "3003"
    NODE_ENV        = $env:NODE_ENV
}

Start-Sleep -Milliseconds 500

Start-Service -Label "ingestion-service (3001)" -Workspace "services/ingestion-service" -Env @{
    DATABASE_URL        = $env:DATABASE_URL
    INGESTION_PORT      = "3001"
    PROCESSING_BASE_URL = "http://localhost:3003"
    NODE_ENV            = $env:NODE_ENV
}

Start-Sleep -Milliseconds 500

Start-Service -Label "realtime-gateway (3004)" -Workspace "services/realtime-gateway" -Env @{
    DATABASE_URL                   = $env:DATABASE_URL
    GATEWAY_PORT                   = "3004"
    REALTIME_GATEWAY_CORS_ORIGIN   = "http://localhost:4200"
    NODE_ENV                       = $env:NODE_ENV
}

# ── Aguardar e verificar saúde ────────────────────────────────────────────────
Write-Host "`n[start-dev] Aguardando 20s para os serviços iniciarem..." -ForegroundColor Cyan
Start-Sleep -Seconds 20

Write-Host "`n══════════════ HEALTH CHECKS ══════════════" -ForegroundColor Cyan
foreach ($check in @(
    @{ Label = "processing :3003/health     "; Url = "http://localhost:3003/health" },
    @{ Label = "ingestion  :3001/health     "; Url = "http://localhost:3001/health" },
    @{ Label = "workspace  POST /workspace/auto"; Url = ""; Post = $true }
)) {
    if ($check.Post) {
        try {
            $body = '{"token":"dev-test-000001"}'
            $resp = Invoke-WebRequest -Uri "http://localhost:3003/workspace/auto" `
                -Method POST -Body $body -ContentType "application/json" -UseBasicParsing
            Write-Host "  $($check.Label) → $($resp.Content)" -ForegroundColor Green
        } catch {
            Write-Host "  $($check.Label) → OFFLINE" -ForegroundColor Red
        }
    } else {
        try {
            $resp = Invoke-WebRequest -Uri $check.Url -UseBasicParsing
            Write-Host "  $($check.Label) → $($resp.Content)" -ForegroundColor Green
        } catch {
            Write-Host "  $($check.Label) → OFFLINE" -ForegroundColor Red
        }
    }
}

Write-Host "`n[start-dev] Done. Próximo passo: npm run frontend" -ForegroundColor Cyan
