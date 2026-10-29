# Request Pacing and Risk Control

[中文](请求频率与风控设计.zh-CN) | English

Once Xianyu's risk control flags an account it rarely recovers quickly, so the helper keeps request volume low **at the source** instead of retrying or cooling down afterwards. This page explains what the program does so you know what to expect.

## What you will notice

- **Every platform (MTOP) request is paced per account.** Requests of the same account start at least 2 seconds apart plus a random jitter; background batch jobs use at least 5 seconds. Order-detail lookups use their own stricter coordinator (at least 5 seconds plus jitter, with a short result cache).
- **One user action or one scan makes at most one, or a few capped, platform requests.** There is no "one request per list row" fan-out. Chat identity lookups and item sync have hard per-run caps; the remainder is filled in later on demand.
- **A batch stops immediately on risk control.** Review, polish, paging and identity batches end at the first `RGV587`/risk verification, expired session or exhausted token. The rest of the batch is not requested and is not replayed automatically after a cooldown.
- **Order sync is incremental.** Orders are stored page by page, and the front end timing out does not cancel the background job.
- **Slow lists are normal.** Large first-time syncs take a while on purpose; do not click refresh repeatedly.

## What you can do to stay safe

- Deploy on a home network or NAS rather than a data-center IP when possible.
- Keep the server clock in sync (NTP).
- Do not run the same account in several helpers or in a browser and the helper at the same time.
- Avoid very large manual syncs at once; spread them out.
- If risk control is triggered, follow [Risk-Control Troubleshooting](Risk-Control-Troubleshooting).

## Where to see it

[Runtime Logs](Runtime-Logs): filter source `mtop` and keyword `RGV587` to confirm a risk-control hit; the account card in [Account Management](Account-Management) also shows the risk status.
