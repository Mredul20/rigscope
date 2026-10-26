# Test harness: runs ps/specs.ps1 under PowerShell 7 on Linux or macOS with fake
# Windows data, to check the script parses and prints the JSON Rigscope expects.
#   pwsh scripts/mock-windows.ps1 -Profile laptop > testdata/specs-laptop.json
param([ValidateSet('laptop','desktop')][string]$Profile = 'laptop', [switch]$Admin)

$MockProfile = $Profile
$env:COMPUTERNAME = $(if ($MockProfile -eq 'laptop') { 'SHAHRIAR-PC' } else { 'GAMING-RIG' })
$env:firmware_type = 'UEFI'

function Get-CimInstance {
  param([Parameter(Position = 0)]$ClassName, $Namespace, $Filter)
  $laptop = ($MockProfile -eq 'laptop')
  switch ($ClassName) {
    'Win32_ComputerSystem' {
      if ($laptop) { [pscustomobject]@{ Manufacturer = 'HP'; Model = 'HP ProBook 440 G5'; SystemFamily = '103C_5336AN HP ProBook'; PCSystemType = [uint16]2; HypervisorPresent = $false; TotalPhysicalMemory = [uint64]8463581184 } }
      else { [pscustomobject]@{ Manufacturer = 'Micro-Star International Co., Ltd.'; Model = 'MS-7C56'; SystemFamily = 'To be filled by O.E.M.'; PCSystemType = [uint16]1; HypervisorPresent = $true; TotalPhysicalMemory = [uint64]17107214336 } }
    }
    'Win32_OperatingSystem' {
      if ($laptop) { [pscustomobject]@{ Caption = 'Microsoft Windows 11 Pro'; Version = '10.0.26100'; BuildNumber = '26100'; OSArchitecture = '64-bit'; InstallDate = [datetime]'2024-11-02 14:10:00'; LastBootUpTime = [datetime]'2026-10-25 09:12:00'; FreePhysicalMemory = [uint64]2412000 } }
      else { [pscustomobject]@{ Caption = 'Microsoft Windows 11 Home'; Version = '10.0.26200'; BuildNumber = '26200'; OSArchitecture = '64-bit'; InstallDate = [datetime]'2025-10-04 18:00:00'; LastBootUpTime = [datetime]'2026-10-26 08:00:00'; FreePhysicalMemory = [uint64]9800000 } }
    }
    'Win32_BIOS' { [pscustomobject]@{ SMBIOSBIOSVersion = $(if ($laptop) { 'Q85 Ver. 01.24.00' } else { 'A.J0' }); ReleaseDate = [datetime]'2023-05-10' } }
    'Win32_BaseBoard' { [pscustomobject]@{ Manufacturer = $(if ($laptop) { 'HP' } else { 'Micro-Star International Co., Ltd.' }); Product = $(if ($laptop) { '8377' } else { 'B550-A PRO (MS-7C56)' }) } }
    'Win32_SystemEnclosure' { [pscustomobject]@{ ChassisTypes = $(if ($laptop) { @([uint16]10) } else { @([uint16]3) }) } }
    'Win32_Processor' {
      if ($laptop) { [pscustomobject]@{ Name = 'Intel(R) Core(TM) i5-7200U CPU @ 2.50GHz'; Manufacturer = 'GenuineIntel'; NumberOfCores = [uint32]2; NumberOfLogicalProcessors = [uint32]4; MaxClockSpeed = [uint32]2712; CurrentClockSpeed = [uint32]2712; L2CacheSize = [uint32]512; L3CacheSize = [uint32]3072; Architecture = [uint16]9; SocketDesignation = 'U3E1'; VirtualizationFirmwareEnabled = $true } }
      else { [pscustomobject]@{ Name = 'AMD Ryzen 5 5600X 6-Core Processor             '; Manufacturer = 'AuthenticAMD'; NumberOfCores = [uint32]6; NumberOfLogicalProcessors = [uint32]12; MaxClockSpeed = [uint32]3701; CurrentClockSpeed = [uint32]3701; L2CacheSize = [uint32]3072; L3CacheSize = [uint32]32768; Architecture = [uint16]9; SocketDesignation = 'AM4'; VirtualizationFirmwareEnabled = $false } }
    }
    'Win32_CacheMemory' {
      if ($laptop) { @([pscustomobject]@{ Level = [uint16]3; InstalledSize = [uint32]128 }, [pscustomobject]@{ Level = [uint16]4; InstalledSize = [uint32]512 }, [pscustomobject]@{ Level = [uint16]5; InstalledSize = [uint32]3072 }) }
      else { @([pscustomobject]@{ Level = [uint16]3; InstalledSize = [uint32]384 }, [pscustomobject]@{ Level = [uint16]4; InstalledSize = [uint32]3072 }, [pscustomobject]@{ Level = [uint16]5; InstalledSize = [uint32]32768 }) }
    }
    'Win32_PerfFormattedData_Counters_ProcessorInformation' { [pscustomobject]@{ ProcessorFrequency = [uint32]$(if ($laptop) { 2712 } else { 3701 }); PercentProcessorPerformance = [uint32]$(if ($laptop) { 113 } else { 124 }) } }
    'Win32_PhysicalMemory' {
      if ($laptop) { @([pscustomobject]@{ Capacity = [uint64]8589934592; Speed = [uint32]2400; ConfiguredClockSpeed = [uint32]2133; Manufacturer = 'Samsung'; PartNumber = 'M471A1K43CB1-CRC    '; DeviceLocator = 'Bottom-Slot 1(left)'; SMBIOSMemoryType = [uint32]26; FormFactor = [uint16]12 }) }
      else { @([pscustomobject]@{ Capacity = [uint64]8589934592; Speed = [uint32]3200; ConfiguredClockSpeed = [uint32]3200; Manufacturer = 'Kingston'; PartNumber = 'KF3200C16D4/8GX'; DeviceLocator = 'DIMM 1'; SMBIOSMemoryType = [uint32]26; FormFactor = [uint16]8 },
                 [pscustomobject]@{ Capacity = [uint64]8589934592; Speed = [uint32]3200; ConfiguredClockSpeed = [uint32]3200; Manufacturer = 'Kingston'; PartNumber = 'KF3200C16D4/8GX'; DeviceLocator = 'DIMM 2'; SMBIOSMemoryType = [uint32]26; FormFactor = [uint16]8 }) }
    }
    'Win32_PhysicalMemoryArray' { [pscustomobject]@{ Use = [uint16]3; MemoryDevices = [uint16]$(if ($laptop) { 2 } else { 4 }) } }
    'Win32_VideoController' {
      if ($laptop) { @([pscustomobject]@{ Name = 'Intel(R) HD Graphics 620'; AdapterRAM = [uint32]1073741824; DriverVersion = '31.0.101.2111'; DriverDate = [datetime]'2022-07-20'; PNPDeviceID = 'PCI\VEN_8086&DEV_5916'; CurrentHorizontalResolution = [uint32]1366; CurrentVerticalResolution = [uint32]768; CurrentRefreshRate = [uint32]60; Status = 'OK' }) }
      else { @([pscustomobject]@{ Name = 'NVIDIA GeForce RTX 3060'; AdapterRAM = [uint32]4293918720; DriverVersion = '32.0.15.6614'; DriverDate = [datetime]'2026-09-02'; PNPDeviceID = 'PCI\VEN_10DE&DEV_2503'; CurrentHorizontalResolution = [uint32]2560; CurrentVerticalResolution = [uint32]1440; CurrentRefreshRate = [uint32]165; Status = 'OK' },
                 [pscustomobject]@{ Name = 'Parsec Virtual Display Adapter'; AdapterRAM = [uint32]0; DriverVersion = '0.45.0.0'; DriverDate = [datetime]'2023-01-01'; PNPDeviceID = 'ROOT\DISPLAY\0000'; CurrentHorizontalResolution = [uint32]0; CurrentVerticalResolution = [uint32]0; CurrentRefreshRate = [uint32]0; Status = 'OK' }) }
    }
    'Win32_LogicalDisk' {
      if ($laptop) { @([pscustomobject]@{ DeviceID = 'C:'; VolumeName = ''; FileSystem = 'NTFS'; Size = [uint64]254721126400; FreeSpace = [uint64]21474836480 }, [pscustomobject]@{ DeviceID = 'D:'; VolumeName = 'Data'; FileSystem = 'NTFS'; Size = [uint64]1000202039296; FreeSpace = [uint64]412316860416 }) }
      else { @([pscustomobject]@{ DeviceID = 'C:'; VolumeName = 'Windows'; FileSystem = 'NTFS'; Size = [uint64]999653638144; FreeSpace = [uint64]402653184000 }) }
    }
    'MSFT_Partition' {
      if ($laptop) { @([pscustomobject]@{ DiskNumber = [uint32]0; DriveLetter = [char]0 }, [pscustomobject]@{ DiskNumber = [uint32]0; DriveLetter = [char]'C' }, [pscustomobject]@{ DiskNumber = [uint32]1; DriveLetter = [char]'D' }) }
      else { @([pscustomobject]@{ DiskNumber = [uint32]0; DriveLetter = [char]'C' }) }
    }
    'MSFT_PhysicalDisk' {
      if ($laptop) { @([pscustomobject]@{ DeviceId = '0'; FriendlyName = 'SAMSUNG MZNLN256HAJQ-000H1'; Model = 'SAMSUNG MZNLN256HAJQ-000H1'; FirmwareVersion = 'MAV22H1Q'; MediaType = [uint16]4; BusType = [uint16]11; Size = [uint64]256060514304; HealthStatus = [uint16]0; SpindleSpeed = [uint32]0 },
                       [pscustomobject]@{ DeviceId = '1'; FriendlyName = 'ST1000LM035-1RK172'; Model = 'ST1000LM035-1RK172'; FirmwareVersion = 'SDM2'; MediaType = [uint16]3; BusType = [uint16]11; Size = [uint64]1000204886016; HealthStatus = [uint16]0; SpindleSpeed = [uint32]5400 }) }
      else { @([pscustomobject]@{ DeviceId = '0'; FriendlyName = 'Samsung SSD 980 1TB'; Model = 'Samsung SSD 980 1TB'; FirmwareVersion = '3B4QFXO7'; MediaType = [uint16]4; BusType = [uint16]17; Size = [uint64]1000204886016; HealthStatus = [uint16]0; SpindleSpeed = [uint32]0 }) }
    }
    'AntiVirusProduct' { [pscustomobject]@{ displayName = 'Windows Defender'; productState = [uint32]397568 } }
    'FirewallProduct' { @() }
    'Win32_PnPEntity' { [pscustomobject]@{ Name = 'Trusted Platform Module 2.0'; Status = 'OK' } }
    'Win32_Tpm' { if ($Admin) { [pscustomobject]@{ SpecVersion = '2.0, 0, 1.38'; IsEnabled_InitialValue = $true; IsActivated_InitialValue = $true } } else { throw 'Access denied' } }
    default { $null }
  }
}

function Get-CimAssociatedInstance {
  param($InputObject, $ResultClassName)
  if (-not $Admin) { throw 'Access denied' }
  if ($InputObject.DeviceId -eq '1') {
    return [pscustomobject]@{ Wear = [byte]0; Temperature = [byte]36; TemperatureMax = [byte]49; PowerOnHours = [uint32]14210; ReadErrorsTotal = [uint64]12; ReadErrorsUncorrected = [uint64]0; WriteErrorsTotal = [uint64]0; WriteErrorsUncorrected = [uint64]0; StartStopCycleCount = [uint32]6120 }
  }
  [pscustomobject]@{ Wear = [byte]$(if ($MockProfile -eq 'laptop') { 23 } else { 4 }); Temperature = [byte]41; TemperatureMax = [byte]68; PowerOnHours = [uint32]$(if ($MockProfile -eq 'laptop') { 11873 } else { 3120 }); ReadErrorsTotal = [uint64]0; ReadErrorsUncorrected = [uint64]0; WriteErrorsTotal = [uint64]0; WriteErrorsUncorrected = [uint64]0; StartStopCycleCount = $null }
}

$regData = @{
  'HKLM:\SOFTWARE\Microsoft\Windows NT\CurrentVersion' = @{ UBR = $(if ($MockProfile -eq 'laptop') { 6899 } else { 9711 }); DisplayVersion = $(if ($MockProfile -eq 'laptop') { '24H2' } else { '25H2' }); EditionID = $(if ($MockProfile -eq 'laptop') { 'Professional' } else { 'Core' }) }
  'HKLM:\SYSTEM\CurrentControlSet\Control\SecureBoot\State' = @{ UEFISecureBootEnabled = $(if ($MockProfile -eq 'laptop') { 0 } else { 1 }) }
  'HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\Policies\System' = @{ EnableLUA = 1; ConsentPromptBehaviorAdmin = 5 }
  'HKLM:\SYSTEM\CurrentControlSet\Control\DeviceGuard\Scenarios\HypervisorEnforcedCodeIntegrity' = @{ Enabled = $(if ($MockProfile -eq 'laptop') { 0 } else { 1 }) }
  'HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\Explorer' = @{ SmartScreenEnabled = 'Warn' }
}
function Get-ItemProperty {
  param([Parameter(Position = 0)]$Path, $Name)
  if ($Path -like '*Class*0000') {
    if ($MockProfile -eq 'laptop') { return [pscustomobject]@{ DriverDesc = 'Intel(R) HD Graphics 620'; 'HardwareInformation.MemorySize' = [byte[]](0, 0, 0, 64) } }
    return [pscustomobject]@{ DriverDesc = 'NVIDIA GeForce RTX 3060'; 'HardwareInformation.qwMemorySize' = [int64]12884901888 }
  }
  if (-not $regData.ContainsKey($Path)) { throw 'not found' }
  $h = $regData[$Path]
  if ($Name -and -not $h.ContainsKey($Name)) { throw 'not found' }
  [pscustomobject]$h
}
function Get-ChildItem { param($Path) @([pscustomobject]@{ PSPath = 'HKLM:\SYSTEM\CurrentControlSet\Control\Class\{4d36e968}\0000' }) }
function Get-MpComputerStatus { param() [pscustomobject]@{ AntivirusEnabled = $true; RealTimeProtectionEnabled = $true; AntivirusSignatureAge = [uint32]0; IsTamperProtected = $true; AMServiceEnabled = $true; AMRunningMode = 'Normal' } }
function Get-NetFirewallProfile { param($PolicyStore) @('Domain', 'Private', 'Public') | ForEach-Object { [pscustomobject]@{ Name = $_; Enabled = 'True' } } }
function Get-HotFix {
  $d = $(if ($MockProfile -eq 'laptop') { [datetime]'2026-08-12' } else { [datetime]'2026-10-14' })
  @([pscustomobject]@{ HotFixID = 'KB5066835'; InstalledOn = $d.AddDays(-30) }, [pscustomobject]@{ HotFixID = $(if ($MockProfile -eq 'laptop') { 'KB5063878' } else { 'KB5070773' }); InstalledOn = $d })
}
function New-Object {
  param([switch]$ComObject, [Parameter(Position = 0)]$TypeName)
  if (-not $ComObject) { return Microsoft.PowerShell.Utility\New-Object $TypeName }
  $o = [pscustomobject]@{}
  $o | Add-Member ScriptMethod NameSpace {
    param($path)
    $self = [pscustomobject]@{}
    $self | Add-Member ScriptMethod ExtendedProperty { param($n) if ($MockProfile -eq 'laptop') { 2 } else { 1 } }
    [pscustomobject]@{ Self = $self }
  }
  $o
}

$errs = $null
$null = [System.Management.Automation.Language.Parser]::ParseFile("$PSScriptRoot/../ps/specs.ps1", [ref]$null, [ref]$errs)
if ($errs.Count) { $errs | ForEach-Object { Write-Error $_.Message }; exit 1 }
. "$PSScriptRoot/../ps/specs.ps1"
