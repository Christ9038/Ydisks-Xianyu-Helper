# Risk-Control Troubleshooting (Q&A)

[中文](风控处理.zh-CN) | English

This page collects the most frequent consultation threads in the issue tracker about Xianyu (Goofish) risk control ("风控"), such as *"Xianyu requires security verification, please scan the QR code again"*, *"API renewal updated the credential, reconnecting…"* or `PERMISSION_EXCEPTION`. It is based on Issues #32, #48, #53, #64, #77, #92, #93 and #100.

> Risk control is a **platform-side** decision about the Cookie / IP / device, not a defect of this program. The program cannot bypass face verification or slider checks for you; it can only keep its own request rate low and let you hand it a Cookie that has already passed verification.

## Why does re-scanning the QR code not help?

**Q: I scanned the QR code many times and always get "Xianyu requires security verification". Why?**

While the account is flagged, every new QR login returns a *new* Cookie that **carries the same risk flag**. Scanning again just produces another flagged Cookie. You must first clear the flag in a real browser, then give the cleared Cookie to the system.

## Recommended recovery procedure

**Q: How do I recover an account whose Cookie is risk-controlled?**

1. Open the Xianyu web version (`goofish.com`) in a desktop browser **on the same network that the program uses** and sign in with that account.
2. Open the **Messages** page. If a slider, face or SMS verification appears, complete it. (If nothing appears, see the next question.)
3. Open the browser developer tools → **Network** panel, refresh the Messages page, and search for `token` in the request list.
4. Click that `token` request and copy the **request header `Cookie`** (the Cookie *sent with the request*, not a Cookie shown on some other tab or in the Application panel).
5. In the management console go to **Account Management → Edit account**, paste the Cookie, save, and confirm the account turns online.
6. Do not re-scan the QR code afterwards; wait until the account is stable.

Never paste your Cookie into an issue, chat or screenshot.

## Troubleshooting checklist

**Q: I pasted the Cookie and it still fails / the saved Cookie disappears after login fails.**

The system discards a Cookie that the platform still rejects. Check, in order:

- Did the web page really pass verification? Reported successes (Issue #92) often involved a slider appearing on the *second* attempt; if the web page never asked for verification, the Cookie was probably copied before the flag was triggered.
- Did you copy the Cookie of the **`token` request** and not just any Cookie?
- Is the browser's network the same as the server's network? A VPN, proxy switch or different region changes the IP and re-triggers verification (Issue #48: the Cookie stopped working within five minutes after switching VPN).
- Do not copy a Cookie while the web page is itself showing a verification page.
- Retry after a few hours. In Issue #93 the account recovered by itself hours later, and a user reported that restarting the container sometimes recovers a flagged account.

**Q: The log says "WS Token is about to expire, rotating proactively" and it never recovers.**

It means the Cookie in use is not a freshly verified one. Redo the procedure above using the Cookie from a request made right after passing verification (Issue #92).

**Q: Order sync fails with `PERMISSION_EXCEPTION::无权限访问` ("no permission").**

The order-detail interface of that Cookie is flagged (Issue #53). Sign in to the official site, open **My Xianyu** and click into a product/order to trigger and pass verification, paste the resulting Cookie back into the account, then sync again. Upgrade to v1.0.13 or later.

**Q: "API renewal updated the credential, reconnecting" keeps repeating.**

That is the protocol-level renewal path. It usually means the credential is still flagged; follow the recovery procedure. Versions 1.0.11 renewed too aggressively; 1.0.12 lowered the frequency — make sure you run a recent version.

## Prevention

**Q: Where should I deploy to avoid risk control?**

At home: a NAS, soft router or home server with Docker. Data-center / cloud IPs (including Hong Kong VPS) are easy for the platform to recognise (Issue #100). Keep the exit IP stable; do not switch VPN/proxy nodes frequently.

**Q: Can I run with `-no-browser`?**

Not recommended. Browser-backed verification and order-detail fallbacks need Chromium; disable it only if Chromium is genuinely unavailable (Issues #48, #77).

**Q: Does the program flood the platform with requests?**

No. All MTOP requests go through a per-account throttler (≥2 s between requests, ≥5 s for background batches), and batch jobs stop immediately when risk control is detected. Do not run several copies against the same account or share one account across machines.

**Q: A transient network error happened, will the account stay offline?**

Dial failures are retried with back-off (5 s, 15 s, 30 s, 60 s, then every 120 s) and only an explicit platform rejection ends the account loop (Issue #80, fixed after v1.0.13). Network errors like `wsarecv ... forcibly closed` or `server misbehaving` (Issues #7, #32) are local network/DNS problems, not API failures.

See also: [Account Management](Account-Management), [FAQ](FAQ), [Deployment and Database](Deployment-and-Database).
