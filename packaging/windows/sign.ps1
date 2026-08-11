param(
    [Parameter(Mandatory = $true)]
    [string]$PfxPath,

    [Parameter(Mandatory = $true)]
    [string[]]$Files
)

$ErrorActionPreference = 'Stop'

$pfxPassword = $env:WINDOWS_SIGNING_PFX_PASSWORD
$description = $env:WINDOWS_SIGNING_IDENTITY
if ([string]::IsNullOrWhiteSpace($pfxPassword)) {
    throw 'WINDOWS_SIGNING_PFX_PASSWORD 未设置'
}
if ([string]::IsNullOrWhiteSpace($description)) {
    throw 'WINDOWS_SIGNING_IDENTITY 未设置'
}

$signtool = Get-ChildItem -Path "${env:ProgramFiles(x86)}\Windows Kits\10\bin" -Filter 'signtool.exe' -Recurse |
    Where-Object { $_.FullName -match '\\x64\\signtool\.exe$' } |
    Sort-Object FullName |
    Select-Object -Last 1
if ($null -eq $signtool) {
    throw 'Windows SDK 中未找到 x64 signtool.exe'
}

foreach ($file in $Files) {
    if (-not (Test-Path -LiteralPath $file -PathType Leaf)) {
        throw "待签名文件不存在: $file"
    }

    & $signtool.FullName sign `
        /f $PfxPath `
        /p $pfxPassword `
        /fd SHA256 `
        /tr 'http://timestamp.acs.microsoft.com' `
        /td SHA256 `
        /d $description `
        $file
    if ($LASTEXITCODE -ne 0) {
        throw "signtool 签名失败: $file"
    }

    $signature = Get-AuthenticodeSignature -FilePath $file
    if ($null -eq $signature.SignerCertificate) {
        throw "未找到 Authenticode 签名: $file"
    }
}
