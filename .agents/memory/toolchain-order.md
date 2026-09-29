---
name: Replit toolchain order
description: Multiple installed Go modules can unexpectedly select the older version in shell and browser tests.
---

When both an older and newer Go module are installed, the shell may select the older compiler even when the newer one is also present. Keep only a compatible Go module for projects that set `GOTOOLCHAIN=local`.

**Why:** A browser test server unexpectedly ran an outdated Go compiler and failed to start, while a previous run had succeeded with the newer compiler. Removing the older module resolved the inconsistent selection.

**How to apply:** If the Go version reported by a workflow or test does not match the project requirement, inspect the active compiler and installed modules before changing application code.