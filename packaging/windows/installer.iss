#define AppVersion GetEnv("APP_VERSION")
#define AppPublisher "Christ9038"
#define AppExeName "xianyu-server.exe"
#define AppDataDir "{commonappdata}\YdisksXianyuHelper"
#define RepoRoot AddBackslash(SourcePath) + "..\.."
#define WindowsDistDir AddBackslash(RepoRoot) + "dist\windows"
#define WindowsRuntimeDir AddBackslash(WindowsDistDir) + "playwright-runtime\amd64"
#define WindowsIconDir AddBackslash(RepoRoot) + "icon\windows"
#define WindowsLanguageDir AddBackslash(SourcePath) + "languages"

[Setup]
AppId={{A6E8B04B-3C8A-4E20-AE62-6B1C3F6B31AE}
AppName={cm:ProductName}
AppVersion={#AppVersion}
AppPublisher={#AppPublisher}
DefaultDirName={autopf}\{cm:ProductName}
DefaultGroupName={cm:ProductName}
OutputBaseFilename=Ydisks-Xianyu-Helper-Setup
PrivilegesRequired=admin
ArchitecturesInstallIn64BitMode=x64compatible
DisableProgramGroupPage=yes
UninstallDisplayIcon={app}\icon.ico
SetupIconFile={#WindowsIconDir}\icon.ico
Compression=lzma2
SolidCompression=yes

[Languages]
Name: "english"; MessagesFile: "compiler:Default.isl"
Name: "chinesesimplified"; MessagesFile: "{#WindowsLanguageDir}\ChineseSimplified.isl"
Name: "chinesetraditional"; MessagesFile: "{#WindowsLanguageDir}\ChineseTraditional.isl"

[CustomMessages]
english.ProductName=Ydisks Xianyu Helper
chinesesimplified.ProductName=Ydisks闲鱼助手
chinesetraditional.ProductName=Ydisks闲鱼助手
english.StartMenuShortcut=Create a Start Menu shortcut
chinesesimplified.StartMenuShortcut=创建开始菜单快捷方式
chinesetraditional.StartMenuShortcut=创建开始菜单快捷方式
english.DesktopShortcut=Create a desktop shortcut
chinesesimplified.DesktopShortcut=创建桌面快捷方式
chinesetraditional.DesktopShortcut=创建桌面快捷方式
english.ShortcutOptions=Shortcut options:
chinesesimplified.ShortcutOptions=快捷方式选项：
chinesetraditional.ShortcutOptions=快捷方式选项：
english.StartTray=Start tray controller
chinesesimplified.StartTray=启动托盘控制器
chinesetraditional.StartTray=启动托盘控制器

[Files]
Source: "{#WindowsDistDir}\xianyu-server.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#WindowsDistDir}\xianyu-tray.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#WindowsIconDir}\icon.ico"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#WindowsRuntimeDir}\*"; DestDir: "{app}\playwright-runtime"; Flags: recursesubdirs createallsubdirs ignoreversion
Source: "service-control.ps1"; Flags: dontcopy
Source: "service-control.ps1"; DestDir: "{app}"; Flags: ignoreversion

[Dirs]
Name: "{#AppDataDir}"
Name: "{#AppDataDir}\data"
Name: "{#AppDataDir}\logs"; Permissions: users-modify

[Registry]
Root: HKCU; Subkey: "Software\Microsoft\Windows\CurrentVersion\Run"; ValueType: string; ValueName: "YdisksXianyuHelperTray"; ValueData: """{app}\xianyu-tray.exe"""; Flags: uninsdeletevalue

[Tasks]
Name: "startmenuicon"; Description: "{cm:StartMenuShortcut}"; GroupDescription: "{cm:ShortcutOptions}"
Name: "desktopicon"; Description: "{cm:DesktopShortcut}"; GroupDescription: "{cm:ShortcutOptions}"; Flags: unchecked

[Icons]
Name: "{group}\{cm:ProductName}"; Filename: "{app}\xianyu-tray.exe"; WorkingDir: "{app}"; IconFilename: "{app}\icon.ico"; Tasks: startmenuicon
Name: "{autodesktop}\{cm:ProductName}"; Filename: "{app}\xianyu-tray.exe"; WorkingDir: "{app}"; IconFilename: "{app}\icon.ico"; Tasks: desktopicon

[Run]
Filename: "{app}\xianyu-tray.exe"; Description: "{cm:StartTray}"; Flags: nowait postinstall skipifsilent runasoriginaluser; Check: ServiceStartedSuccessfully

[UninstallRun]
Filename: "{sys}\WindowsPowerShell\v1.0\powershell.exe"; Parameters: "-NoLogo -NoProfile -NonInteractive -ExecutionPolicy Bypass -File ""{app}\service-control.ps1"" -Mode uninstall -TrayPath ""{app}\xianyu-tray.exe"""; Flags: runhidden waituntilterminated

[Code]
var
  ServiceRegistered: Boolean;
  ServiceStartFailed: Boolean;
  InstallationCompleted: Boolean;

function ServiceScriptParameters(const ScriptPath, Mode: String): String;
begin
  Result := '-NoLogo -NoProfile -NonInteractive -ExecutionPolicy Bypass -File "' +
    ScriptPath + '" -Mode ' + Mode +
    ' -ExePath "' + ExpandConstant('{app}\xianyu-server.exe') +
    '" -TrayPath "' + ExpandConstant('{app}\xianyu-tray.exe') +
    '" -WorkDir "' + ExpandConstant('{commonappdata}\YdisksXianyuHelper') +
    '" -RuntimeRoot "' + ExpandConstant('{app}\playwright-runtime') + '"';
end;

function PrepareToInstall(var NeedsRestart: Boolean): String;
var
  ResultCode: Integer;
begin
  Result := '';
  ExtractTemporaryFile('service-control.ps1');
  if not Exec(
    ExpandConstant('{sys}\WindowsPowerShell\v1.0\powershell.exe'),
    '-NoLogo -NoProfile -NonInteractive -ExecutionPolicy Bypass -File "' +
      ExpandConstant('{tmp}\service-control.ps1') + '" -Mode stop -TrayPath "' +
      ExpandConstant('{app}\xianyu-tray.exe') + '"',
    '', SW_HIDE, ewWaitUntilTerminated, ResultCode)
  then
    Result := '无法执行旧服务停止脚本。'
  else if ResultCode <> 0 then
    Result := '旧版后台服务停止失败，错误码：' + IntToStr(ResultCode) + '。';
end;

procedure RegisterWindowsService;
var
  ResultCode: Integer;
  PowerShellPath: String;
  Parameters: String;
begin
  PowerShellPath := ExpandConstant('{sys}\WindowsPowerShell\v1.0\powershell.exe');
  Parameters := ServiceScriptParameters(
    ExpandConstant('{tmp}\service-control.ps1'), 'register') +
    ' -CreatedMarkerPath "' + ExpandConstant('{tmp}\service-created.marker') + '"';

  if not Exec(PowerShellPath, Parameters, '', SW_HIDE, ewWaitUntilTerminated, ResultCode) then
  begin
    Log('Unable to execute Windows service registration script.');
    SuppressibleMsgBox(
      '无法执行 Windows 服务注册脚本。请以管理员身份重新运行安装器。',
      mbError, MB_OK, IDOK);
    Abort;
  end;
  if ResultCode <> 0 then
  begin
    Log('Windows service registration failed, result code: ' + IntToStr(ResultCode));
    SuppressibleMsgBox(
      'Windows 服务注册失败（错误码：' + IntToStr(ResultCode) + '）。',
      mbError, MB_OK, IDOK);
    Abort;
  end;
  ServiceRegistered := True;
end;

procedure StartWindowsService;
var
  ResultCode: Integer;
  PowerShellPath: String;
  Parameters: String;
begin
  PowerShellPath := ExpandConstant('{sys}\WindowsPowerShell\v1.0\powershell.exe');
  Parameters := ServiceScriptParameters(
    ExpandConstant('{app}\service-control.ps1'), 'start');

  ServiceStartFailed :=
    (not Exec(PowerShellPath, Parameters, '', SW_HIDE,
      ewWaitUntilTerminated, ResultCode)) or (ResultCode <> 0);
  if ServiceStartFailed then
  begin
    Log('Windows service failed to start, result code: ' + IntToStr(ResultCode));
    SuppressibleMsgBox(
      'Windows 后台服务未能启动（错误码：' + IntToStr(ResultCode) +
      '）。安装器将返回失败，请查看安装日志。',
      mbError, MB_OK, IDOK);
  end;
end;

procedure CleanupCreatedWindowsService;
var
  ResultCode: Integer;
  ScriptPath: String;
begin
  if not FileExists(ExpandConstant('{tmp}\service-created.marker')) then
    exit;
  ScriptPath := ExpandConstant('{tmp}\service-control.ps1');
  if FileExists(ScriptPath) then
    Exec(
      ExpandConstant('{sys}\WindowsPowerShell\v1.0\powershell.exe'),
      ServiceScriptParameters(ScriptPath, 'uninstall'),
      '', SW_HIDE, ewWaitUntilTerminated, ResultCode);
end;

function ServiceStartedSuccessfully: Boolean;
begin
  Result := ServiceRegistered and (not ServiceStartFailed);
end;

function GetCustomSetupExitCode: Integer;
begin
  if ServiceStartFailed then
    Result := 1001
  else
    Result := 0;
end;

procedure CurStepChanged(CurStep: TSetupStep);
begin
  if CurStep = ssInstall then
    RegisterWindowsService
  else if CurStep = ssPostInstall then
    StartWindowsService
  else if CurStep = ssDone then
    InstallationCompleted := True;
end;

procedure DeinitializeSetup;
begin
  if not InstallationCompleted then
    CleanupCreatedWindowsService;
end;
