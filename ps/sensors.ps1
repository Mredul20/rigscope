# Rigscope sensor helper.
#
# Runs in the background while Rigscope is open and prints one JSON line every
# two seconds. Like specs.ps1 it only READS: sensor apps' WMI data, ACPI thermal
# zones and the battery's charge rate. Rigscope ends it when the window closes.
#
# Exact processor temperatures need a hardware driver, which Rigscope doesn't
# ship. If LibreHardwareMonitor or OpenHardwareMonitor is running, their
# readings are used.

$ErrorActionPreference = 'SilentlyContinue'
$ProgressPreference = 'SilentlyContinue'
[Console]::OutputEncoding = [Text.Encoding]::UTF8

function FindSensorApp {
  foreach ($n in 'root/LibreHardwareMonitor', 'root/OpenHardwareMonitor') {
    $s = Get-CimInstance -Namespace $n -ClassName Sensor -ErrorAction SilentlyContinue | Select-Object -First 1
    if ($s) { return $n }
  }
  return $null
}

$ns = FindSensorApp
$tick = 0
while ($true) {
  $o = [ordered]@{ app = ''; sensors = @(); acpi = @(); battery = $null }
  # look for a sensor app again every 30 seconds, in case it was started later
  if (-not $ns -and ($tick % 15) -eq 0) { $ns = FindSensorApp }
  if ($ns) {
    $o.app = $(if ($ns -like '*Libre*') { 'LibreHardwareMonitor' } else { 'OpenHardwareMonitor' })
    $list = @(Get-CimInstance -Namespace $ns -ClassName Sensor -ErrorAction SilentlyContinue |
      Where-Object { $_.SensorType -eq 'Temperature' -or $_.SensorType -eq 'Fan' -or $_.SensorType -eq 'Power' } |
      ForEach-Object { [ordered]@{ n = [string]$_.Name; t = [string]$_.SensorType; p = [string]$_.Parent; v = [double]$_.Value } })
    if ($list.Count -eq 0) { $ns = $null } else { $o.sensors = $list }
  }
  try {
    $o.acpi = @(Get-CimInstance -Namespace root/wmi -ClassName MSAcpi_ThermalZoneTemperature -ErrorAction Stop |
      ForEach-Object { [ordered]@{ name = [string]$_.InstanceName; value = [math]::Round(([double]$_.CurrentTemperature / 10) - 273.15, 1) } })
  } catch { }
  try {
    $b = Get-CimInstance -Namespace root/wmi -ClassName BatteryStatus -ErrorAction Stop | Select-Object -First 1
    if ($b) { $o.battery = [ordered]@{ charge = [int64]$b.ChargeRate; discharge = [int64]$b.DischargeRate } }
  } catch { }
  [Console]::Out.WriteLine((ConvertTo-Json -InputObject $o -Depth 4 -Compress))
  [Console]::Out.Flush()
  $tick++
  Start-Sleep -Seconds 2
}
