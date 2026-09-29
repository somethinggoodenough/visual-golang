---
name: GitHub connector and Git pushes
description: An attached GitHub connector can write through the REST API without authenticating the local Git CLI.
---

Do not assume an attached GitHub connector makes `git push` over HTTPS work. The connector's authenticated REST proxy may work even when Git's credential helper still rejects a push.

**Why:** A connected account could create Git objects and update a branch through the GitHub API, but a normal Git push still failed authentication.

**How to apply:** First try a normal non-interactive push. If only Git authentication fails, use the connector's Git Data API to upload the local commits without exposing credentials. Verify each resulting object hash against the local commit and advance the remote ref only as a non-forced fast-forward.