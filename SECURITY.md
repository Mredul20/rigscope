# Security policy

Rigscope reads information about the PC it runs on. It's designed so that nothing it reads can leave that PC, and so that it never changes anything.

## What Rigscope promises

- It only reads. It doesn't change settings, write to the registry, or delete or modify files. It writes only two things: the temporary battery report from `powercfg`, which it deletes right after reading, and the app window's browser profile in `%LOCALAPPDATA%\Rigscope`.
- It makes no network requests. No telemetry, analytics or update checks.
- Its interface is served on `127.0.0.1` only, on a random port, and every request needs a random token created when the app starts. Requests with any other host name are refused, which blocks DNS rebinding.
- While it's open, Rigscope runs two read-only helpers: a PowerShell script (`ps/sensors.ps1`) that reads temperatures and the battery's charge rate, and NVIDIA's `nvidia-smi` on PCs with NVIDIA graphics. Both belong to a Windows job object, so Windows ends them as soon as Rigscope closes.
- It never ends, pauses or changes other programs. The "What's using your PC" list only reads.
- *Run as administrator* restarts Rigscope with the standard Windows prompt. Rigscope never asks for or handles passwords.

If you find a way to break any of these, that's a security bug.

## Reporting a problem

Please don't open a public issue for security problems. Instead, report it privately through GitHub: open the **Security** tab of this repository and click **Report a vulnerability**.

Please include the Rigscope version (`Rigscope.exe --version`), your Windows version and the steps to reproduce. You'll get a reply within a week. Once a fix is released, you'll be credited in the release notes unless you'd rather not be.

## Supported versions

Only the latest release gets security fixes.

## Checking a download

Each release lists SHA-256 checksums in `SHA256SUMS.txt`. In PowerShell:

```
Get-FileHash .\Rigscope.exe -Algorithm SHA256
```

The result should match the line for `Rigscope.exe`. Releases are built by the `release` workflow in `.github/workflows/`, straight from the tagged source.
