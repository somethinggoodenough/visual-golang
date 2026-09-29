---
name: Temporary test port mappings
description: Replit can re-add browser-test server ports to workspace configuration after a workflow restart.
---

Browser tests that launch their own web server can cause Replit to add a temporary port mapping. That mapping may reappear after the app workflow restarts, even if it was removed earlier.

**Why:** Cleaning the test mapping before restarting the application did not stick; cleaning it after the last restart left the preview port intact and the workspace free of test-only changes.

**How to apply:** Finish browser tests and workflow restarts first, then remove any test-only port mapping through the validated configuration replacement flow and check the final diff.