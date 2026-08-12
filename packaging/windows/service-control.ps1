param(
    [Parameter(Mandatory = $true)]
    [ValidateSet('stop', 'install', 'uninstall')]
    [string]$Mode,

    [string]$ServiceName = 'YdisksXianyuHelper',
    [string]$ExePath = '',
    [string]$TrayPath = '',
    [string]$WorkDir = '',
    [string]$RuntimeRoot = ''
)

$ErrorActionPreference = 'Stop'

function Get-InstalledService {
    Get-Service -Name $ServiceName -ErrorAction SilentlyContinue
}

function Stop-InstalledService {
    $service = Get-InstalledService
    if ($null -eq $service) {
        return
    }
    if ($service.Status -ne [System.ServiceProcess.ServiceControllerStatus]::Stopped) {
        Stop-Service -Name $ServiceName -ErrorAction Stop
        $service.WaitForStatus(
            [System.ServiceProcess.ServiceControllerStatus]::Stopped,
            [TimeSpan]::FromSeconds(30)
        )
    }
}

function Stop-InstalledTray {
    if ([string]::IsNullOrWhiteSpace($TrayPath)) {
        return
    }
    $normalizedTrayPath = [IO.Path]::GetFullPath($TrayPath)
    Get-Process -Name 'xianyu-tray' -ErrorAction SilentlyContinue | ForEach-Object {
        $processPath = $null
        try {
            $processPath = $_.Path
        } catch {
            $processPath = $null
        }
        if (-not [string]::IsNullOrWhiteSpace($processPath) -and
            [StringComparer]::OrdinalIgnoreCase.Equals(
                [IO.Path]::GetFullPath($processPath),
                $normalizedTrayPath
            )) {
            Stop-Process -Id $_.Id -Force -ErrorAction Stop
            if (-not $_.WaitForExit(10000)) {
                throw "等待旧托盘进程退出超时: $($_.Id)"
            }
        }
    }
}

function Stop-InstalledComponents {
    Stop-InstalledTray
    Stop-InstalledService
}

function Invoke-ScChecked {
    param([Parameter(ValueFromRemainingArguments = $true)][string[]]$Arguments)

    $output = & "$env:SystemRoot\System32\sc.exe" @Arguments 2>&1
    if ($LASTEXITCODE -ne 0) {
        throw "sc.exe $($Arguments -join ' ') 失败 ($LASTEXITCODE): $($output -join ' ')"
    }
}

function Grant-InteractiveUserServiceControl {
    # IU（INTERACTIVE）只包含当前交互式登录会话。授予的 LCRPWP 分别是
    # 查询状态、启动和停止；不包含修改配置、修改 ACL 或删除服务。
    $controlAce = '(A;;LCRPWP;;;IU)'
    $output = & "$env:SystemRoot\System32\sc.exe" sdshow $ServiceName 2>&1
    if ($LASTEXITCODE -ne 0) {
        throw "读取服务安全描述符失败 ($LASTEXITCODE): $($output -join ' ')"
    }
    $sddl = $output |
        ForEach-Object { $_.ToString().Trim() } |
        Where-Object { $_ -like 'D:*' } |
        Select-Object -First 1
    if ([string]::IsNullOrWhiteSpace($sddl)) {
        throw '服务安全描述符中没有找到 DACL'
    }
    if ($sddl.Contains($controlAce)) {
        return
    }

    $saclIndex = $sddl.IndexOf('S:')
    if ($saclIndex -ge 0) {
        $updatedSddl = $sddl.Insert($saclIndex, $controlAce)
    } else {
        $updatedSddl = $sddl + $controlAce
    }
    Invoke-ScChecked sdset $ServiceName $updatedSddl
}

switch ($Mode) {
    'stop' {
        Stop-InstalledComponents
    }
    'install' {
        if ([string]::IsNullOrWhiteSpace($ExePath) -or
            [string]::IsNullOrWhiteSpace($WorkDir) -or
            [string]::IsNullOrWhiteSpace($RuntimeRoot)) {
            throw '安装服务缺少 ExePath、WorkDir 或 RuntimeRoot'
        }

        Stop-InstalledComponents
        $binaryPath = '"{0}" -service -workdir "{1}" -data-key-file "{1}\data-key" -addr 127.0.0.1:59188 -playwright-runtime-root "{2}"' -f `
            $ExePath, $WorkDir, $RuntimeRoot
        if ($null -eq (Get-InstalledService)) {
            Invoke-ScChecked create $ServiceName 'binPath=' $binaryPath 'start=' 'delayed-auto'
        } else {
            Invoke-ScChecked config $ServiceName 'binPath=' $binaryPath 'start=' 'delayed-auto'
        }
        Invoke-ScChecked description $ServiceName 'Ydisks闲鱼助手后台服务'
        Grant-InteractiveUserServiceControl

        Start-Service -Name $ServiceName -ErrorAction Stop
        (Get-Service -Name $ServiceName).WaitForStatus(
            [System.ServiceProcess.ServiceControllerStatus]::Running,
            [TimeSpan]::FromSeconds(30)
        )
    }
    'uninstall' {
        Stop-InstalledComponents
        if ($null -ne (Get-InstalledService)) {
            Invoke-ScChecked delete $ServiceName
        }
    }
}
