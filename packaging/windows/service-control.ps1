param(
    [Parameter(Mandatory = $true)]
    [ValidateSet('stop', 'register', 'start', 'install', 'uninstall')]
    [string]$Mode,

    [string]$ServiceName = 'YdisksXianyuHelper',
    [string]$ExePath = '',
    [string]$TrayPath = '',
    [string]$WorkDir = '',
    [string]$RuntimeRoot = '',
    [string]$CreatedMarkerPath = '',
    [string]$FailureLogPath = ''
)

$ErrorActionPreference = 'Stop'

function Get-InstalledService {
    Get-Service -Name $ServiceName -ErrorAction SilentlyContinue
}

function Test-ServiceInstalled {
    $service = Get-InstalledService
    if ($null -eq $service) {
        return $false
    }
    $service.Dispose()
    return $true
}

function Stop-InstalledService {
    $service = Get-InstalledService
    if ($null -eq $service) {
        return
    }
    try {
        if ($service.Status -ne [System.ServiceProcess.ServiceControllerStatus]::Stopped) {
            Stop-Service -Name $ServiceName -ErrorAction Stop
            $service.WaitForStatus(
                [System.ServiceProcess.ServiceControllerStatus]::Stopped,
                [TimeSpan]::FromSeconds(30)
            )
        }
    } finally {
        $service.Dispose()
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

function Wait-ServiceDeleted {
    $deadline = [DateTime]::UtcNow.AddSeconds(30)
    while (Test-ServiceInstalled) {
        if ([DateTime]::UtcNow -ge $deadline) {
            throw "等待 Windows 服务 $ServiceName 删除超时"
        }
        Start-Sleep -Milliseconds 250
    }
}

function Invoke-ScChecked {
    param([Parameter(ValueFromRemainingArguments = $true)][string[]]$Arguments)

    $output = & "$env:SystemRoot\System32\sc.exe" @Arguments 2>&1
    if ($LASTEXITCODE -ne 0) {
        throw "sc.exe $($Arguments -join ' ') 失败 ($LASTEXITCODE): $($output -join ' ')"
    }
}

function Mark-InstalledServiceForDeletion {
    $output = & "$env:SystemRoot\System32\sc.exe" delete $ServiceName 2>&1
    # ERROR_SERVICE_MARKED_FOR_DELETE (1072) 表示前一轮删除已经成功提交，
    # 只需继续等待 SCM 释放所有句柄，不能把它当作升级失败。
    if ($LASTEXITCODE -eq 0 -or $LASTEXITCODE -eq 1072) {
        return
    }
    throw "sc.exe delete $ServiceName 失败 ($LASTEXITCODE): $($output -join ' ')"
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

function Assert-ServiceInstallParameters {
    if ([string]::IsNullOrWhiteSpace($ExePath) -or
        [string]::IsNullOrWhiteSpace($WorkDir) -or
        [string]::IsNullOrWhiteSpace($RuntimeRoot)) {
        throw '安装服务缺少 ExePath、WorkDir 或 RuntimeRoot'
    }
}

function Update-InstalledServiceConfiguration {
    param([Parameter(Mandatory = $true)][string]$BinaryPath)

    $service = Get-CimInstance -ClassName Win32_Service -Filter "Name='$ServiceName'" -ErrorAction Stop
    if ($null -eq $service) {
        throw "Windows 服务 $ServiceName 不存在，无法更新可执行命令"
    }
    $changeResult = Invoke-CimMethod -InputObject $service -MethodName Change -Arguments @{ PathName = $BinaryPath } -ErrorAction Stop
    if ($changeResult.ReturnValue -ne 0) {
        throw "更新 Windows 服务 $ServiceName 的可执行命令失败，Win32 返回码: $($changeResult.ReturnValue)"
    }
    Invoke-ScChecked config $ServiceName 'start=' 'delayed-auto'
    Invoke-ScChecked description $ServiceName 'Ydisks Xianyu Helper background service'
    Grant-InteractiveUserServiceControl
}

function Get-ExceptionNativeErrorCode {
    param([Parameter(Mandatory = $true)][System.Exception]$Exception)

    $currentException = $Exception
    while ($null -ne $currentException) {
        if ($currentException -is [System.ComponentModel.Win32Exception]) {
            return $currentException.NativeErrorCode
        }
        $currentException = $currentException.InnerException
    }
    return $null
}

function New-InstalledService {
    param([Parameter(Mandatory = $true)][string]$BinaryPath)

    $deadline = [DateTime]::UtcNow.AddSeconds(30)
    while ($true) {
        try {
            New-Service -Name $ServiceName `
                -BinaryPathName $BinaryPath `
                -DisplayName 'Ydisks Xianyu Helper' `
                -StartupType Automatic `
                -ErrorAction Stop | Out-Null
            return
        } catch {
            $nativeErrorCode = Get-ExceptionNativeErrorCode -Exception $_.Exception
            if ($nativeErrorCode -ne 1072 -or [DateTime]::UtcNow -ge $deadline) {
                throw
            }
            # DeleteService 仅标记删除；其他进程尚未关闭服务句柄时，CreateService
            # 会返回 ERROR_SERVICE_MARKED_FOR_DELETE (1072)，短暂重试可避免升级竞态。
            Start-Sleep -Milliseconds 250
        }
    }
}

function Create-InstalledService {
    param([Parameter(Mandatory = $true)][string]$BinaryPath)

    if (-not [string]::IsNullOrWhiteSpace($CreatedMarkerPath)) {
        Set-Content -LiteralPath $CreatedMarkerPath -Value $ServiceName -Encoding ASCII
    }
    New-InstalledService -BinaryPath $BinaryPath
    Update-InstalledServiceConfiguration -BinaryPath $BinaryPath
}

function Recreate-InstalledService {
    param([Parameter(Mandatory = $true)][string]$BinaryPath)

    Mark-InstalledServiceForDeletion
    Wait-ServiceDeleted
    Create-InstalledService -BinaryPath $BinaryPath
}

function Write-ServiceControlFailure {
    param([Parameter(Mandatory = $true)][System.Management.Automation.ErrorRecord]$Failure)

    if ([string]::IsNullOrWhiteSpace($FailureLogPath)) {
        return
    }
    try {
        $failureDirectory = Split-Path -Parent $FailureLogPath
        if (-not [string]::IsNullOrWhiteSpace($failureDirectory)) {
            New-Item -ItemType Directory -Force -Path $failureDirectory | Out-Null
        }
        $failureText = @(
            "时间: $([DateTime]::Now.ToString('o'))"
            "模式: $Mode"
            "服务: $ServiceName"
            '异常:'
            ($Failure | Out-String).Trim()
        ) -join [Environment]::NewLine
        Set-Content -LiteralPath $FailureLogPath -Value $failureText -Encoding UTF8
    } catch {
        Write-Warning "无法写入 Windows 服务失败日志: $($_.Exception.Message)"
    }
}

function Register-InstalledService {
    Assert-ServiceInstallParameters
    Stop-InstalledComponents

    New-Item -ItemType Directory -Force -Path $WorkDir | Out-Null
    New-Item -ItemType Directory -Force -Path (Join-Path $WorkDir 'data') | Out-Null
    New-Item -ItemType Directory -Force -Path (Join-Path $WorkDir 'logs') | Out-Null

    $binaryPath = '"{0}" -service -workdir "{1}" -data-key-file "{1}\data-key" -addr 127.0.0.1:59188 -playwright-runtime-root "{2}"' -f `
        $ExePath, $WorkDir, $RuntimeRoot
    if (-not (Test-ServiceInstalled)) {
        Create-InstalledService -BinaryPath $binaryPath
    } else {
        try {
            Update-InstalledServiceConfiguration -BinaryPath $binaryPath
        } catch {
            # 旧版本可能留下拒绝修改配置或 ACL 的服务对象。服务本身无用户数据，
            # 删除后重建即可恢复固定的命令行、延迟启动和交互用户启停权限。
            Write-Warning "更新现有 Windows 服务 $ServiceName 失败，正在删除后重建: $($_.Exception.Message)"
            Recreate-InstalledService -BinaryPath $binaryPath
        }
    }

    if (-not (Test-ServiceInstalled)) {
        throw "Windows 服务 $ServiceName 注册后无法查询"
    }
}

function Start-InstalledService {
    $service = Get-InstalledService
    if ($null -eq $service) {
        throw "Windows 服务 $ServiceName 尚未注册"
    }

    try {
        if ($service.Status -eq [System.ServiceProcess.ServiceControllerStatus]::StopPending) {
            $service.WaitForStatus(
                [System.ServiceProcess.ServiceControllerStatus]::Stopped,
                [TimeSpan]::FromSeconds(30)
            )
            $service.Refresh()
        }
        if ($service.Status -eq [System.ServiceProcess.ServiceControllerStatus]::StartPending) {
            $service.WaitForStatus(
                [System.ServiceProcess.ServiceControllerStatus]::Running,
                [TimeSpan]::FromSeconds(30)
            )
            $service.Refresh()
        }
        if ($service.Status -ne [System.ServiceProcess.ServiceControllerStatus]::Running) {
            Start-Service -Name $ServiceName -ErrorAction Stop
            $service.Refresh()
            $service.WaitForStatus(
                [System.ServiceProcess.ServiceControllerStatus]::Running,
                [TimeSpan]::FromSeconds(30)
            )
            $service.Refresh()
        }
        if ($service.Status -ne [System.ServiceProcess.ServiceControllerStatus]::Running) {
            throw "Windows 服务 $ServiceName 未进入运行状态: $($service.Status)"
        }
    } finally {
        $service.Dispose()
    }
}

try {
    switch ($Mode) {
        'stop' {
            Stop-InstalledComponents
        }
        'register' {
            Register-InstalledService
        }
        'start' {
            Start-InstalledService
        }
        'install' {
            Register-InstalledService
            Start-InstalledService
        }
        'uninstall' {
            Stop-InstalledComponents
            if (Test-ServiceInstalled) {
                Mark-InstalledServiceForDeletion
                Wait-ServiceDeleted
            }
        }
    }
} catch {
    Write-ServiceControlFailure -Failure $_
    throw
}
