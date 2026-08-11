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

[Dirs]
Name: "{#AppDataDir}\data"

[Registry]
Root: HKCU; Subkey: "Software\Microsoft\Windows\CurrentVersion\Run"; ValueType: string; ValueName: "YdisksXianyuHelperTray"; ValueData: "{app}\xianyu-tray.exe"; Flags: uninsdeletevalue

[Run]
Filename: "{sys}\sc.exe"; Parameters: "create YdisksXianyuHelper binPath= ""{app}\xianyu-server.exe"" -service -workdir {#AppDataDir} -data-key-file {#AppDataDir}\data-key -addr 127.0.0.1:8080 -playwright-runtime-root ""{app}\playwright-runtime"" start= delayed-auto"; Flags: runhidden waituntilterminated
Filename: "{sys}\sc.exe"; Parameters: "start YdisksXianyuHelper"; Flags: runhidden waituntilterminated
Filename: "{app}\xianyu-tray.exe"; Description: "启动菜单栏控制器"; Flags: nowait postinstall skipifsilent

[UninstallRun]
Filename: "{sys}\sc.exe"; Parameters: "stop YdisksXianyuHelper"; Flags: runhidden waituntilterminated
Filename: "{sys}\sc.exe"; Parameters: "delete YdisksXianyuHelper"; Flags: runhidden waituntilterminated
