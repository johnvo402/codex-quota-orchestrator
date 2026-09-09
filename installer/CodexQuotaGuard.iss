#ifndef AppVersion
  #define AppVersion "dev"
#endif

#define AppName "Codex Desktop Quota Guard"
#define AppPublisher "johnvo402"
#define AppURL "https://github.com/johnvo402/codex-quota-orchestrator"
#define AppExeName "orch.exe"

[Setup]
AppId={{B6D7EA9B-4A17-4A08-9D9D-D11791236051}
AppName={#AppName}
AppVersion={#AppVersion}
AppPublisher={#AppPublisher}
AppPublisherURL={#AppURL}
AppSupportURL={#AppURL}
AppUpdatesURL={#AppURL}
DefaultDirName={localappdata}\CodexQuotaGuard
DefaultGroupName={#AppName}
DisableProgramGroupPage=yes
PrivilegesRequired=lowest
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
OutputDir=..\dist
OutputBaseFilename=CodexQuotaGuardSetup
Compression=lzma2
SolidCompression=yes
WizardStyle=modern
CloseApplications=yes
RestartApplications=no
ChangesEnvironment=yes
UninstallDisplayName={#AppName}
UninstallDisplayIcon={app}\bin\orch.exe
VersionInfoVersion={#AppVersion}
VersionInfoCompany={#AppPublisher}
VersionInfoDescription={#AppName} Setup
VersionInfoProductName={#AppName}
VersionInfoProductVersion={#AppVersion}

[InstallDelete]
Type: files; Name: "{app}\Uninstall.exe"
Type: filesandordirs; Name: "{app}\.upgrade-backup"

[Files]
Source: "..\bin\orchestrator.exe"; DestDir: "{app}\bin"; Flags: ignoreversion
Source: "..\bin\orchestrator.exe"; DestDir: "{app}\bin"; DestName: "orch.exe"; Flags: ignoreversion
Source: "..\bin\orchestrator-daemon.exe"; DestDir: "{app}\bin"; Flags: ignoreversion
Source: "..\bin\desktop-companion.exe"; DestDir: "{app}\bin"; Flags: ignoreversion
Source: "..\config.example.json"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\README.md"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\docs\WINDOWS_SETUP.md"; DestDir: "{app}\docs"; Flags: ignoreversion
Source: "..\docs\ARCHITECTURE.md"; DestDir: "{app}\docs"; Flags: ignoreversion
Source: "..\docs\TROUBLESHOOTING.md"; DestDir: "{app}\docs"; Flags: ignoreversion

[Registry]
Root: HKCU; Subkey: "Software\Microsoft\Windows\CurrentVersion\Uninstall\CodexQuotaGuard"; Flags: deletekey dontcreatekey
Root: HKCU; Subkey: "Software\Microsoft\Windows\CurrentVersion\Run"; ValueType: string; ValueName: "CodexQuotaGuard"; ValueData: "{app}\bin\orchestrator-daemon.exe daemon"; Flags: uninsdeletevalue

[Icons]
Name: "{group}\Codex Quota Guard Dashboard"; Filename: "{app}\bin\orch.exe"; Parameters: "ui"

[Run]
Filename: "{app}\bin\orchestrator-daemon.exe"; Parameters: "daemon"; StatusMsg: "Starting quota daemon..."; Flags: runhidden nowait
Filename: "{app}\bin\orch.exe"; Parameters: "ui"; Description: "Open Codex Quota Guard dashboard"; Flags: postinstall nowait skipifsilent unchecked

[UninstallRun]
Filename: "{app}\bin\orch.exe"; Parameters: "teardown"; Flags: runhidden waituntilterminated; RunOnceId: "CodexQuotaGuardTeardown"

[Code]
const
  EnvironmentKey = 'Environment';
  PathValueName = 'Path';

function NormalizePathEntry(Value: string): string;
begin
  Result := RemoveQuotes(Trim(Value));
  while (Length(Result) > 3) and (Result[Length(Result)] = '\') do
    Delete(Result, Length(Result), 1);
  Result := Lowercase(Result);
end;

function PathContains(CurrentPath, Entry: string): Boolean;
var
  Parts: TArrayOfString;
  I: Integer;
begin
  Result := False;
  Parts := SplitString(CurrentPath, ';');
  for I := 0 to GetArrayLength(Parts) - 1 do
    if NormalizePathEntry(Parts[I]) = NormalizePathEntry(Entry) then
    begin
      Result := True;
      Exit;
    end;
end;

procedure AddToUserPath(Entry: string);
var
  CurrentPath: string;
begin
  if not RegQueryStringValue(HKCU, EnvironmentKey, PathValueName, CurrentPath) then
    CurrentPath := '';
  if PathContains(CurrentPath, Entry) then
    Exit;
  if (CurrentPath <> '') and (CurrentPath[Length(CurrentPath)] <> ';') then
    CurrentPath := CurrentPath + ';';
  RegWriteStringValue(HKCU, EnvironmentKey, PathValueName, CurrentPath + Entry);
end;

procedure RemoveFromUserPath(Entry: string);
var
  CurrentPath, NewPath: string;
  Parts: TArrayOfString;
  I: Integer;
begin
  if not RegQueryStringValue(HKCU, EnvironmentKey, PathValueName, CurrentPath) then
    Exit;
  Parts := SplitString(CurrentPath, ';');
  NewPath := '';
  for I := 0 to GetArrayLength(Parts) - 1 do
  begin
    if (Trim(Parts[I]) <> '') and
       (NormalizePathEntry(Parts[I]) <> NormalizePathEntry(Entry)) then
    begin
      if NewPath <> '' then
        NewPath := NewPath + ';';
      NewPath := NewPath + Trim(Parts[I]);
    end;
  end;
  RegWriteStringValue(HKCU, EnvironmentKey, PathValueName, NewPath);
end;

procedure ConfigureCodexIntegration;
var
  ResultCode: Integer;
  Ok: Boolean;
begin
  Ok := Exec(
    ExpandConstant('{app}\bin\orch.exe'),
    'setup',
    ExpandConstant('{app}\bin'),
    SW_HIDE,
    ewWaitUntilTerminated,
    ResultCode
  );
  if (not Ok) or (ResultCode <> 0) then
    MsgBox(
      'Application files were installed, but Codex integration setup did not complete.' + #13#10 + #13#10 +
      'Make sure Codex CLI is installed and logged in, then open a new terminal and run:' + #13#10 +
      '  orch setup',
      mbError,
      MB_OK
    );
end;

procedure CurStepChanged(CurStep: TSetupStep);
begin
  if CurStep = ssPostInstall then
  begin
    AddToUserPath(ExpandConstant('{app}\bin'));
    ConfigureCodexIntegration;
  end;
end;

procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
begin
  if CurUninstallStep = usUninstall then
    RemoveFromUserPath(ExpandConstant('{app}\bin'));
end;
