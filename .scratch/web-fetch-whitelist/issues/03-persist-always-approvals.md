# 03 — Persist "always" approvals across restart

**What to build:** Choosing "allow always" for a host now adds it to the
whitelist permanently. The next request for that host fetches straight away
without prompting, and that remains true after the server is restarted.

Only a human choosing "always" may add a host. No other path writes to the
whitelist — see ADR-0001.

If the whitelist cannot be saved, the call the human just approved still
succeeds, because they did approve it. But the host is not treated as
permanently allowed, the failure is logged, and the server keeps running.

**Blocked by:** 02.

**Status:** ready-for-agent

- [ ] Choosing "allow always" adds the host and saves the whitelist
- [ ] A later request for that host fetches without prompting
- [ ] That still holds after restarting the server
- [ ] Choosing "allow once" leaves the saved whitelist unchanged
- [ ] Denying, cancelling, or failing leaves the saved whitelist unchanged
- [ ] If saving fails, the approved call still succeeds, the host is not treated as permanently allowed, the failure is logged, and the server does not crash
- [ ] Hosts are stored in the same canonical form used for matching, so a saved approval matches on the next request
- [ ] Concurrent requests can read and add hosts without data races
- [ ] Restart persistence is covered by a test that reloads from the saved file
