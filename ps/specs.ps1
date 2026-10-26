# Rigscope hardware and security collector.
#
# This script only READS information. It never changes a setting, writes a file
# or connects to the network. Rigscope runs it with Windows PowerShell 5.1
# (built into Windows 10 and 11) and parses the single JSON object it prints.
#
# Every section is wrapped in try/catch so one failing query (for example on a
# PC where a WMI class is missing) never hides the rest of the report.

$ErrorActionPreference = 'SilentlyContinue'
$ProgressPreference = 'SilentlyContinue'
[Console]::OutputEncoding = [Text.Encoding]::UTF8

function S($v) { if ($null -eq $v) { '' } else { ([string]$v).Trim() } }
function D($v) { if ($v -is [datetime]) { $v.ToString('yyyy-MM-ddTHH:mm:ss') } else { '' } }
function N($v) { if ($null -eq $v) { -1 } else { [int64]$v } }
function RegVal($path, $name) {
  try { (Get-ItemProperty -Path $path -Name $name -ErrorAction Stop).$name } catch { $null }
}

$out = [ordered]@{}
$errs = New-Object System.Collections.Generic.List[string]

# ---- System and Windows -------------------------------------------------------
try {
  $cs = Get-CimInstance Win32_ComputerSystem
  $os = Get-CimInstance Win32_OperatingSystem
  $bios = Get-CimInstance Win32_BIOS
  $bb = Get-CimInstance Win32_BaseBoard
  $chassis = @(Get-CimInstance Win32_SystemEnclosure | ForEach-Object { $_.ChassisTypes } | Where-Object { $null -ne $_ } | ForEach-Object { [int]$_ })
  $cv = 'HKLM:\SOFTWARE\Microsoft\Windows NT\CurrentVersion'
  $out.system = [ordered]@{
    computerName = S $env:COMPUTERNAME
    manufacturer = S $cs.Manufacturer
    model        = S $cs.Model
    family       = S $cs.SystemFamily
    pcType       = [int]$cs.PCSystemType
    chassis      = $chassis
    board        = (S ((S $bb.Manufacturer) + ' ' + (S $bb.Product)))
    bios         = S $bios.SMBIOSBIOSVersion
    biosDate     = D $bios.ReleaseDate
    firmware     = S $env:firmware_type
    hypervisor   = [bool]$cs.HypervisorPresent
    totalMemory  = [int64]$cs.TotalPhysicalMemory
  }
  $out.os = [ordered]@{
    caption        = S $os.Caption
    version        = S $os.Version
    build          = [int]$os.BuildNumber
    ubr            = [int](RegVal $cv 'UBR')
    displayVersion = S (RegVal $cv 'DisplayVersion')
    edition        = S (RegVal $cv 'EditionID')
    arch           = S $os.OSArchitecture
    installDate    = D $os.InstallDate
    lastBoot       = D $os.LastBootUpTime
    freeMemoryKB   = [int64]$os.FreePhysicalMemory
  }
} catch { $errs.Add('system: ' + $_.Exception.Message) }

# ---- Processor ------------------------------------------------------------------
try {
  $cpus = @(Get-CimInstance Win32_Processor)
  $l1 = [int64]0; $l2 = [int64]0; $l3 = [int64]0
  # Win32_CacheMemory levels: 3 = L1 (primary), 4 = L2, 5 = L3. Sizes are in KB.
  foreach ($c in @(Get-CimInstance Win32_CacheMemory)) {
    switch ([int]$c.Level) {
      3 { $l1 += [int64]$c.InstalledSize }
      4 { $l2 += [int64]$c.InstalledSize }
      5 { $l3 += [int64]$c.InstalledSize }
    }
  }
  $l2p = [int64](($cpus | Measure-Object L2CacheSize -Sum).Sum)
  $l3p = [int64](($cpus | Measure-Object L3CacheSize -Sum).Sum)
  # The performance counter gives the real current speed, including turbo boost.
  $perf = Get-CimInstance Win32_PerfFormattedData_Counters_ProcessorInformation -Filter "Name='_Total'"
  $cur = [int]$cpus[0].CurrentClockSpeed
  if ($perf -and $perf.ProcessorFrequency) {
    $cur = [int]([double]$perf.ProcessorFrequency * [double]$perf.PercentProcessorPerformance / 100)
  }
  $out.cpu = [ordered]@{
    name         = S $cpus[0].Name
    maker        = S $cpus[0].Manufacturer
    sockets      = $cpus.Count
    cores        = [int](($cpus | Measure-Object NumberOfCores -Sum).Sum)
    threads      = [int](($cpus | Measure-Object NumberOfLogicalProcessors -Sum).Sum)
    baseMHz      = [int]$cpus[0].MaxClockSpeed
    currentMHz   = $cur
    l1KB         = $l1
    l2KB         = $(if ($l2p -gt 0) { $l2p } else { $l2 })
    l3KB         = $(if ($l3p -gt 0) { $l3p } else { $l3 })
    arch         = [int]$cpus[0].Architecture
    socket       = S $cpus[0].SocketDesignation
    virtFirmware = [bool]$cpus[0].VirtualizationFirmwareEnabled
  }
} catch { $errs.Add('cpu: ' + $_.Exception.Message) }

# ---- Memory ---------------------------------------------------------------------
try {
  $mods = @(Get-CimInstance Win32_PhysicalMemory | ForEach-Object {
    [ordered]@{
      capacity   = [int64]$_.Capacity
      speed      = [int]$_.Speed
      configured = [int]$_.ConfiguredClockSpeed
      maker      = S $_.Manufacturer
      part       = S $_.PartNumber
      slot       = S $_.DeviceLocator
      type       = [int]$_.SMBIOSMemoryType
      formFactor = [int]$_.FormFactor
    }
  })
  $slots = [int]((@(Get-CimInstance Win32_PhysicalMemoryArray | Where-Object { $_.Use -eq 3 }) | Measure-Object MemoryDevices -Sum).Sum)
  $out.memory = [ordered]@{ modules = $mods; slots = $slots }
} catch { $errs.Add('memory: ' + $_.Exception.Message) }

# ---- Graphics -------------------------------------------------------------------
try {
  # Win32_VideoController caps memory at 4 GB, so read the real amount from the driver's registry key.
  $vram = @{}
  $class = 'HKLM:\SYSTEM\CurrentControlSet\Control\Class\{4d36e968-e325-11ce-bfc1-08002be10318}'
  foreach ($k in @(Get-ChildItem $class -ErrorAction SilentlyContinue)) {
    $p = Get-ItemProperty $k.PSPath -ErrorAction SilentlyContinue
    if (-not $p -or -not $p.DriverDesc) { continue }
    $q = $p.'HardwareInformation.qwMemorySize'
    if ($null -eq $q) {
      $q = $p.'HardwareInformation.MemorySize'
      if ($q -is [byte[]] -and $q.Length -ge 4) { $q = [BitConverter]::ToUInt32($q, 0) }
    }
    if ($null -ne $q -and -not ($q -is [byte[]])) { $vram[(S $p.DriverDesc)] = [int64]$q }
  }
  $out.gpus = @(Get-CimInstance Win32_VideoController | ForEach-Object {
    $n = S $_.Name
    $mem = [int64]$_.AdapterRAM
    if ($vram.ContainsKey($n) -and $vram[$n] -gt $mem) { $mem = $vram[$n] }
    [ordered]@{
      name       = $n
      memory     = $mem
      driver     = S $_.DriverVersion
      driverDate = D $_.DriverDate
      pnp        = S $_.PNPDeviceID
      width      = [int]$_.CurrentHorizontalResolution
      height     = [int]$_.CurrentVerticalResolution
      refresh    = [int]$_.CurrentRefreshRate
      status     = S $_.Status
    }
  })
} catch { $errs.Add('gpu: ' + $_.Exception.Message) }

# ---- Storage --------------------------------------------------------------------
try {
  $out.volumes = @(Get-CimInstance Win32_LogicalDisk -Filter 'DriveType=3' | ForEach-Object {
    [ordered]@{ letter = S $_.DeviceID; label = S $_.VolumeName; fs = S $_.FileSystem; size = [int64]$_.Size; free = [int64]$_.FreeSpace }
  })
} catch { $errs.Add('volumes: ' + $_.Exception.Message) }

try {
  # Storage Management API: the same data as Settings > System > Storage > Drive health.
  # Wear, temperature and error counters are only shared with administrator apps.
  $ns = 'root/Microsoft/Windows/Storage'
  $letters = @{}
  foreach ($p in @(Get-CimInstance -Namespace $ns -ClassName MSFT_Partition)) {
    $l = ([string]$p.DriveLetter).Trim([char]0).Trim()
    if ($l.Length -gt 0) {
      $k = [string]$p.DiskNumber
      if (-not $letters.ContainsKey($k)) { $letters[$k] = @() }
      $letters[$k] += ($l + ':')
    }
  }
  $out.disks = @(foreach ($d in @(Get-CimInstance -Namespace $ns -ClassName MSFT_PhysicalDisk)) {
    $r = $null; $relErr = ''
    try { $r = @(Get-CimAssociatedInstance -InputObject $d -ResultClassName MSFT_StorageReliabilityCounter -ErrorAction Stop)[0] }
    catch { $relErr = $_.Exception.Message }
    $id = [string]$d.DeviceId
    [ordered]@{
      id = $id; name = S $d.FriendlyName; model = S $d.Model; firmware = S $d.FirmwareVersion
      media = [int]$d.MediaType; bus = [int]$d.BusType; size = [int64]$d.Size
      health = [int]$d.HealthStatus; spindle = [int64]$d.SpindleSpeed
      letters = @($letters[$id] | Where-Object { $_ })
      rel = ($null -ne $r); relErr = $relErr
      wear = $r.Wear; temp = $r.Temperature; tempMax = $r.TemperatureMax; hours = $r.PowerOnHours
      readErr = $r.ReadErrorsTotal; readErrUnc = $r.ReadErrorsUncorrected
      writeErr = $r.WriteErrorsTotal; writeErrUnc = $r.WriteErrorsUncorrected
      startStop = $r.StartStopCycleCount
    }
  })
} catch { $errs.Add('disks: ' + $_.Exception.Message) }

# ---- Security -------------------------------------------------------------------
$sec = [ordered]@{}
try {
  # Windows Security Center (client editions of Windows only).
  $sec.av = @(Get-CimInstance -Namespace root/SecurityCenter2 -ClassName AntiVirusProduct | ForEach-Object {
    [ordered]@{ name = S $_.displayName; state = [int64]$_.productState }
  })
  $sec.fw3p = @(Get-CimInstance -Namespace root/SecurityCenter2 -ClassName FirewallProduct | ForEach-Object {
    [ordered]@{ name = S $_.displayName; state = [int64]$_.productState }
  })
} catch { $errs.Add('securitycenter: ' + $_.Exception.Message) }
try {
  $mp = Get-MpComputerStatus -ErrorAction Stop
  if ($mp) {
    $sec.defender = [ordered]@{
      enabled = [bool]$mp.AntivirusEnabled; realtime = [bool]$mp.RealTimeProtectionEnabled
      sigAge = [int]$mp.AntivirusSignatureAge; tamper = [bool]$mp.IsTamperProtected
      service = [bool]$mp.AMServiceEnabled; mode = S $mp.AMRunningMode
    }
  }
} catch { }
try {
  $sec.firewall = @(Get-NetFirewallProfile -PolicyStore ActiveStore | ForEach-Object {
    [ordered]@{ name = S $_.Name; enabled = ((S $_.Enabled) -eq 'True') }
  })
} catch { $errs.Add('firewall: ' + $_.Exception.Message) }
try {
  $sec.secureBoot = N (RegVal 'HKLM:\SYSTEM\CurrentControlSet\Control\SecureBoot\State' 'UEFISecureBootEnabled')
  $tpm = @(Get-CimInstance Win32_PnPEntity -Filter "PNPClass='SecurityDevices'" | Where-Object { (S $_.Name) -match 'Trusted Platform|TPM' })
  $sec.tpmName = $(if ($tpm.Count) { S $tpm[0].Name } else { '' })
  $sec.tpmOK = [bool]($tpm.Count -and (S $tpm[0].Status) -eq 'OK')
  try {
    $t = Get-CimInstance -Namespace root/cimv2/Security/MicrosoftTpm -ClassName Win32_Tpm -ErrorAction Stop
    if ($t) { $sec.tpmSpec = S $t.SpecVersion; $sec.tpmReady = [bool]($t.IsEnabled_InitialValue -and $t.IsActivated_InitialValue) }
  } catch { }
} catch { $errs.Add('tpm: ' + $_.Exception.Message) }
try {
  # Explorer's BitLocker status works without administrator rights.
  # 1 = on, 2 = off, 3/4 = encrypting or decrypting, 5 = suspended, 6 = on and locked, 8 = waiting for activation.
  $shell = New-Object -ComObject Shell.Application
  $sec.bitlocker = @(Get-CimInstance Win32_LogicalDisk -Filter 'DriveType=3' | ForEach-Object {
    $v = $null
    try { $v = $shell.NameSpace($_.DeviceID + '\').Self.ExtendedProperty('System.Volume.BitLockerProtection') } catch { }
    [ordered]@{ letter = S $_.DeviceID; state = (N $v) }
  })
} catch { $errs.Add('bitlocker: ' + $_.Exception.Message) }
try {
  $hf = @(Get-HotFix | Where-Object { $_.InstalledOn } | Sort-Object InstalledOn -Descending)
  $sec.lastUpdate = $(if ($hf.Count) { D $hf[0].InstalledOn } else { '' })
  $sec.lastUpdateId = $(if ($hf.Count) { S $hf[0].HotFixID } else { '' })
} catch { $errs.Add('updates: ' + $_.Exception.Message) }
try {
  $pol = 'HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\Policies\System'
  $sec.uac = N (RegVal $pol 'EnableLUA')
  $sec.uacPrompt = N (RegVal $pol 'ConsentPromptBehaviorAdmin')
  $sec.memoryIntegrity = N (RegVal 'HKLM:\SYSTEM\CurrentControlSet\Control\DeviceGuard\Scenarios\HypervisorEnforcedCodeIntegrity' 'Enabled')
  $sec.smartScreen = S (RegVal 'HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\Explorer' 'SmartScreenEnabled')
  $sec.smartScreenPolicy = N (RegVal 'HKLM:\SOFTWARE\Policies\Microsoft\Windows\System' 'EnableSmartScreen')
} catch { $errs.Add('policies: ' + $_.Exception.Message) }
$out.security = $sec

$out.errors = @($errs)
ConvertTo-Json -InputObject $out -Depth 6 -Compress
