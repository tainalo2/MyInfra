; The setup of MyInfra for Windows: the app without a console, in the
; programs of the person, with no administrator rights, so that MyInfra can
; update itself there. deploy/windows/package.ps1 calls it.
;   iscc /DVersion=1.4.0 /DExe=C:\path\myinfra_windows_amd64_app.exe /DArch=x64compatible /DOut=MyInfra_1.4.0_windows_amd64_setup myinfra.iss

#ifndef Version
  #define Version "0.0.0"
#endif
#ifndef Arch
  #define Arch "x64compatible"
#endif
#ifndef Out
  #define Out "MyInfra_setup"
#endif
#ifndef OutDir
  #define OutDir "."
#endif

[Setup]
AppId={{74B44110-FE33-4C69-A57D-B766A8BD9D76}
AppName=MyInfra
AppVersion={#Version}
AppVerName=MyInfra {#Version}
AppPublisher=MyInfra
AppPublisherURL=https://tainalo2.github.io/MyInfra/
AppSupportURL=https://github.com/tainalo2/MyInfra/issues
AppUpdatesURL=https://github.com/tainalo2/MyInfra/releases
VersionInfoVersion={#Version}
DefaultDirName={autopf}\MyInfra
PrivilegesRequired=lowest
DisableProgramGroupPage=yes
DisableDirPage=yes
OutputDir={#OutDir}
OutputBaseFilename={#Out}
Compression=lzma2/max
SolidCompression=yes
WizardStyle=modern
ArchitecturesAllowed={#Arch}
ArchitecturesInstallIn64BitMode={#Arch}
MinVersion=10.0
UninstallDisplayIcon={app}\MyInfra.exe
UninstallDisplayName=MyInfra
; A running MyInfra is closed as its window would be: it stops PostgreSQL.
CloseApplications=yes
RestartApplications=no
ShowLanguageDialog=auto

[Languages]
Name: "en"; MessagesFile: "compiler:Default.isl"
Name: "fr"; MessagesFile: "compiler:Languages\French.isl"
Name: "de"; MessagesFile: "compiler:Languages\German.isl"
Name: "it"; MessagesFile: "compiler:Languages\Italian.isl"
Name: "pt"; MessagesFile: "compiler:Languages\Portuguese.isl"

[Tasks]
Name: "desktopicon"; Description: "{cm:CreateDesktopIcon}"; GroupDescription: "{cm:AdditionalIcons}"; Flags: unchecked

[Files]
Source: "{#Exe}"; DestDir: "{app}"; DestName: "MyInfra.exe"; Flags: ignoreversion

[Icons]
Name: "{autoprograms}\MyInfra"; Filename: "{app}\MyInfra.exe"
Name: "{autodesktop}\MyInfra"; Filename: "{app}\MyInfra.exe"; Tasks: desktopicon

[Run]
Filename: "{app}\MyInfra.exe"; Description: "{cm:LaunchProgram,MyInfra}"; Flags: nowait postinstall skipifsilent

; What an update leaves beside the executable. The data of MyInfra, in its
; own folder, stays: a new install finds it again.
[UninstallDelete]
Type: files; Name: "{app}\MyInfra.exe.old"
Type: files; Name: "{app}\MyInfra.exe.new"
