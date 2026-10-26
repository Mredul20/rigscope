# Contributing to Rigscope

Thanks for helping. The most useful contributions are usually small ones: a missing game, a graphics chip Rigscope doesn't recognize, or a requirement that's out of date.

## Add or fix a game or app

Everything is in [`catalog/catalog.json`](catalog/catalog.json). An entry looks like this:

```json
{"id": "eldenring", "name": "Elden Ring", "kind": "game", "category": "Action RPG",
 "min": {"cpu": {"name": "Intel Core i5-8400", "cores": 6, "threads": 6}, "gpu": "GeForce GTX 1060 3GB", "ram": 12},
 "rec": {"cpu": {"name": "Intel Core i7-8700K", "cores": 6, "threads": 12}, "gpu": "GeForce GTX 1070", "ram": 16},
 "storage": 60}
```

- Copy the **minimum** and **recommended** specs from the publisher's official page (Steam, the game's site or the app's help pages). When they list an Intel and an AMD option, use the Intel processor and the NVIDIA graphics card, so all entries compare the same way.
- Processors need their core and thread counts. Look them up on Intel's or AMD's product pages.
- `ram` and `storage` are in GB.
- Leave out `rec` if the publisher only gives a minimum.
- Optional flags: `"ssd": true` (needs or strongly recommends an SSD), `"needsTpmSecureBoot": true` (anti-cheat refuses to start without TPM 2.0 and Secure Boot), `"needsTpmSecureBootWin11": true` (the same, but only on Windows 11), `"needsVirtualization": true`, `"noArm": true` (doesn't run on ARM processors), and `"note"` for anything a user should know.
- Mention your source in the pull request.

Then run `go test ./...`. The tests fail if a processor or graphics card in your entry isn't one Rigscope can score, and tell you which.

## Explain a common process

To add a plain-English note for a process people often wonder about in "What's using your PC", add it to the `notes` map in `monitor.go`, keyed by the lowercase program name (for example `"searchindexer.exe"`). Keep it to one sentence about what it does and whether it's normal.

## Add a graphics chip

Add a line to the `gpus` list in the same file: `{"m": "rtx 5060", "s": 330}`.

- `m` is the part of the name to match, in lowercase, without "NVIDIA", "GeForce", "AMD", "Intel" or "(R)"/"(TM)". AMD cards are written `rx 7600`, Intel's `arc a770`.
- `s` is the score, relative to a GeForce GTX 1060 at 100. Use a widely published 3DMark Time Spy graphics score or a reputable review's relative-performance chart, and divide by the GTX 1060's result in the same source.
- Add `"i": true` for graphics built into the processor.
- Laptop versions: add a separate entry with `laptop` in the match (NVIDIA) or the `m` suffix (AMD, Intel), like `rtx 4060 laptop` or `rx 7600m`. Otherwise Rigscope scores the laptop version at 82% of the desktop card.

The longest matching entry wins, so `rtx 4060 ti` is picked over `rtx 4060`.

## Windows support dates

When Microsoft releases a new Windows version or publishes new end-of-servicing dates, update the `windows` section of the catalog from Microsoft's [Windows 11 release information](https://learn.microsoft.com/en-us/windows/release-health/windows11-release-information) page. `latestUbr` is the number after the dot in the newest build, which tells Rigscope whether a PC is up to date.

## Working on the code

You need [Go](https://go.dev/dl/) 1.22 or newer. There are no other dependencies.

| File | What it does |
|---|---|
| `ps/specs.ps1` | The PowerShell script that reads hardware and security information on Windows |
| `report.go` | Turns the script's output into the report the window shows |
| `score.go` | Processor and graphics scoring |
| `catalog.go` | Loads the catalog and rates each game and app |
| `security.go` | The security checks and the Windows 11 hardware check |
| `battery.go`, `disks.go` | Battery report parsing and drive details |
| `monitor.go` | Live monitoring shared logic: grouping programs, picking temperatures, notes for common processes |
| `monitor_windows.go` | Live readings on Windows: performance counters, the process list, sensor and NVIDIA helpers |
| `monitor_other.go` | Simulated live data for working on the window outside Windows |
| `ps/sensors.ps1` | The background helper that reads temperatures, fans and battery charge rate |
| `sys_windows.go` | Windows-only code: running PowerShell, power status, opening the window |
| `sys_other.go` | Lets Rigscope run on Linux and macOS with sample data, for development |
| `ui/index.html` | The specs and checks view, with no outside libraries |
| `ui/live.js`, `ui/live.css` | The live control panel and its animated circuit board |

Run it on any operating system with sample data:

```
go run .                          # sample laptop
RIGSCOPE_SAMPLE=desktop go run .  # sample gaming desktop
RIGSCOPE_ADMIN=1 go run .         # pretend to be running as administrator
```

On Windows, `go run .` reads the real PC.

### Changing the PowerShell script

`ps/specs.ps1` must keep working in Windows PowerShell 5.1, which is what Windows includes. Avoid newer syntax such as `??`, `?.` and ternary operators. Every section sits in its own `try`/`catch` so one failure never hides the rest.

You can test it without Windows using PowerShell 7 and the fake data in `scripts/mock-windows.ps1`:

```
pwsh scripts/mock-windows.ps1 -Profile laptop  > testdata/specs-laptop.json
pwsh scripts/mock-windows.ps1 -Profile desktop > testdata/specs-desktop.json
```

The CI workflow also runs the real script on a Windows machine for every pull request.

## Pull requests

- Keep each pull request to one change.
- Run `gofmt -w .` and `go test ./...` first.
- Rigscope must stay read-only, offline and driver-free. Pull requests that add network access, telemetry, kernel drivers or anything that changes the PC (including ending processes) won't be accepted.
