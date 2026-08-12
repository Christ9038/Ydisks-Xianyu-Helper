#define AppName "Ydisks Xianyu Helper"
#define AppVersion GetEnv("APP_VERSION")
#define AppPublisher "Christ9038"
#define AppExeName "xianyu-server.exe"
#define AppDataDir "{commonappdata}\YdisksXianyuHelper"

[Setup]
AppId={{A6E8B04B-3C8A-4E20-AE62-6B1C3F6B31AE}
AppName={#AppName}
AppVersion={#AppVersion}
AppPublisher={#AppPublisher}
DefaultDirName={autopf}\Ydisks Xianyu Helper
DefaultGroupName={#AppName}
OutputBaseFilename=Ydisks-Xianyu-Helper-Setup
PrivilegesRequired=admin
ArchitecturesInstallIn64BitMode=x64compatible
DisableProgramGroupPage=yes
UninstallDisplayIcon={app}\icon.ico
SetupIconFile=..\..\icon\windows\icon.ico
Compression=lzma2
SolidCompression=yes

[Files]
Source: "..\..\dist\windows\xianyu-server.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\..\dist\windows\xianyu-tray.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\..\icon\windows\icon.ico"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\..\dist\windows\playwright-runtime\*"; DestDir: "{app}\playwright-runtime"; Flags: recursesubdirs createallsubdirs ignoreversion
Source: "service-control.ps1"; Flags: dontcopy
Source: "service-control.ps1"; DestDir: "{app}"; Flags: ignoreversion

[Dirs]
Name: "{#AppDataDir}"
Name: "{#AppDataDir}\data"
Name: "{#AppDataDir}\logs"; Permissions: users-modify

[Registry]
Root: HKCU; Subkey: "Software\Microsoft\Windows\CurrentVersion\Run"; ValueType: string; ValueName: "YdisksXianyuHelperTray"; ValueData: """{app}\xianyu-tray.exe"""; Flags: uninsdeletevalue

[Run]
Filename: "{sys}\WindowsPowerShell\v1.0\powershell.exe"; Parameters: "-NoLogo -NoProfile -NonInteractive -ExecutionPolicy Bypass -File ""{app}\service-control.ps1"" -Mode install -ExePath ""{app}\xianyu-server.exe"" -TrayPath ""{app}\xianyu-tray.exe"" -WorkDir ""{#AppDataDir}"" -RuntimeRoot ""{app}\playwright-runtime"""; Flags: runhidden waituntilterminated
Filename: "{app}\xianyu-tray.exe"; Description: "启动菜单栏控制器"; Flags: nowait postinstall skipifsilent runasoriginaluser

[UninstallRun]
Filename: "{sys}\WindowsPowerShell\v1.0\powershell.exe"; Parameters: "-NoLogo -NoProfile -NonInteractive -ExecutionPolicy Bypass -File ""{app}\service-control.ps1"" -Mode uninstall -TrayPath ""{app}\xianyu-tray.exe"""; Flags: runhidden waituntilterminated

[Code]
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
