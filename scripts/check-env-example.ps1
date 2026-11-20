param(
    [string]$EnvFile,
    [string]$ExampleFile
)
$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
if (-not $EnvFile)     { $EnvFile = Join-Path $root '.env' }
if (-not $ExampleFile) { $ExampleFile = Join-Path $root '.env.example' }

function Get-ActiveKeys([string]$path) {
    if (-not (Test-Path -LiteralPath $path)) { throw "File tidak ditemukan: $path" }
    $keys = New-Object System.Collections.Generic.List[string]
    foreach ($line in Get-Content -LiteralPath $path) {
        $t = $line.Trim()
        if ($t -eq '' -or $t.StartsWith('#')) { continue }
        if ($t -match '^([A-Za-z_][A-Za-z0-9_]*)\s*=') { $keys.Add($matches[1]) }
    }
    return ,$keys
}

$envKeys = Get-ActiveKeys $EnvFile
$exampleKeys = Get-ActiveKeys $ExampleFile

Write-Output "Membandingkan nama variabel (aktif, berurutan). Nilai tidak pernah dibaca/ditampilkan."
Write-Output ("  .env         : {0}" -f (Split-Path $EnvFile -Leaf))
Write-Output ("  .env.example : {0}" -f (Split-Path $ExampleFile -Leaf))
Write-Output ''

$identical = $true
$envSet = @{}
foreach ($k in $envKeys) { $envSet[$k] = $true }
$exampleSet = @{}
foreach ($k in $exampleKeys) { $exampleSet[$k] = $true }

foreach ($k in $envKeys) {
    if (-not $exampleSet.ContainsKey($k)) {
        $identical = $false
        Write-Output "HANYA di .env         : $k"
    }
}
foreach ($k in $exampleKeys) {
    if (-not $envSet.ContainsKey($k)) {
        $identical = $false
        Write-Output "HANYA di .env.example : $k"
    }
}

$commonEnv = @($envKeys | Where-Object { $exampleSet.ContainsKey($_) })
$commonExample = @($exampleKeys | Where-Object { $envSet.ContainsKey($_) })
$orderSame = ($commonEnv.Count -eq $commonExample.Count)
if ($orderSame) {
    for ($i = 0; $i -lt $commonEnv.Count; $i++) {
        if ($commonEnv[$i] -ne $commonExample[$i]) { $orderSame = $false; break }
    }
}
if (-not $orderSame) {
    $identical = $false
    Write-Output 'URUTAN BEDA           : urutan variabel aktif tidak sama'
}

if ($identical) {
    foreach ($k in $envKeys) { Write-Output "SAMA                  : $k" }
    Write-Output ''
    Write-Output ("HASIL: IDENTIK ({0} variabel aktif, nama dan urutan sama)" -f $envKeys.Count)
    exit 0
} else {
    Write-Output ''
    Write-Output 'HASIL: BEDA'
    exit 1
}
