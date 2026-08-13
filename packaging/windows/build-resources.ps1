$ErrorActionPreference = 'Stop'

$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot '..\..')).Path
$iconPath = (Resolve-Path (Join-Path $repoRoot 'icon\windows\icon.ico')).Path
$winresTool = 'github.com/tc-hib/go-winres@v0.3.3'

$resourceSpecs = @(
    @{ Package = 'cmd\server'; Output = 'rsrc'; Manifest = 'cli'; Description = 'Ydisks Xianyu Helper server'; Filename = 'xianyu-server.exe' },
    @{ Package = 'cmd\tray'; Output = 'rsrc'; Manifest = 'gui'; Description = 'Ydisks Xianyu Helper tray'; Filename = 'xianyu-tray.exe' }
)

foreach ($spec in $resourceSpecs) {
    $outputPrefix = Join-Path $repoRoot (Join-Path $spec.Package $spec.Output)
    $outputFile = "${outputPrefix}_windows_amd64.syso"
    Remove-Item -LiteralPath $outputFile -Force -ErrorAction SilentlyContinue

    & go run $winresTool simply `
        --arch amd64 `
        --out $outputPrefix `
        --manifest $spec.Manifest `
        --icon $iconPath `
        --product-name 'Ydisks Xianyu Helper' `
        --file-description $spec.Description `
        --original-filename $spec.Filename
    if ($LASTEXITCODE -ne 0) {
        throw "生成 Windows 资源失败: $($spec.Package)"
    }
    if (-not (Test-Path -LiteralPath $outputFile -PathType Leaf)) {
        throw "Windows 资源文件未生成: $outputFile"
    }
}
