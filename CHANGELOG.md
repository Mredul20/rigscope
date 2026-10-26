# Changelog

## 1.1.0

- New live control panel: an animated circuit board of the PC with per-thread load, temperature, clock speed, memory, graphics, disk, network and battery, updated every second.
- Processor heat: temperature gauge, throttling, power, per-core temperatures and fans, with a two-minute history. Uses LibreHardwareMonitor or OpenHardwareMonitor when running, otherwise Windows' thermal sensor. NVIDIA graphics temperature, fan and power via `nvidia-smi`.
- "What's using your PC": running programs grouped like Task Manager, with processor, memory, disk and network, and graphics use, plus plain-English notes for common Windows processes.
- Activity graphs for processor, memory, graphics, disk and network.
- `--live` prints one live sample as JSON.
- Helper processes are ended by Windows automatically when Rigscope closes.

## 1.0.0

First release.

- Processor, memory, graphics, storage and battery details, with live processor and memory use.
- "What can this PC run?" for 23 games and 17 apps.
- Security checks with fixes, plus the Windows 11 hardware check.
- Copy summary, save report and `--json` output.
