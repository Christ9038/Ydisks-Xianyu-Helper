# What to do when your account is risk-controlled

[中文](风控处理.zh-CN) | English

If an account card shows "Xianyu requires security verification, please scan the QR code again", or "API renewal updated the credential, reconnecting…", or order sync fails with `PERMISSION_EXCEPTION`, it is almost always the same thing: **Xianyu has flagged that account's login credential (the Cookie) as risky.**

That is the platform's decision, not a bug. The program cannot do face or slider verification for you. What you can do is pass the verification in a browser yourself, then hand the program the Cookie that has passed.

## Why re-scanning the QR code does not help

While the account is flagged, every new QR login gives you a new Cookie that **carries the same risk flag**. Scanning again changes nothing, so stop re-scanning and use the steps below.

## Step by step (Chrome / Edge)

**Step 1 – Sign in to the Xianyu web version and pass verification**

1. On a computer, open [goofish.com](https://www.goofish.com) and sign in with the flagged account.
2. Open the **Messages** page. If a slider, face or SMS check appears, complete it.
3. Use a computer on the **same network (same exit IP)** as the program if you can. If the program runs on a home NAS but you verify over mobile data or a VPN, the changed IP may trigger verification again.

**Step 2 – Find the `token` request in developer tools**

1. On the Messages page press `F12` (on Mac `Option + Command + I`) and open the **Network** tab.
2. Tick "Disable cache".
3. Type `token` in the filter box.
4. **Press `F5` to reload** while the panel is open, otherwise the requests are not recorded.
5. Several rows appear. Click the one named like `1.0/?jsv=2.7.2&appKe...`. The row starting with `auth?token=...` is *not* the one.

**Step 3 – Copy the Cookie from the request headers**

1. Click that request and choose **Headers** on the right.
2. Scroll to the **Request Headers** section and find the **Cookie** line.
3. Copy the **whole value**. It is a long string separated by semicolons (`xxx=...; xxx=...; ...`) that usually contains fields such as `unb=` and `_m_h5_tk=`. Right-click the line and choose "Copy value", or double-click the value, select all and copy.

> The Cookie you need is the one **sent with this request** (Request Headers). It is not the Cookie shown in the Application/Storage panel, and not the `set-cookie` in the response headers. Copying from the wrong place is the most common reason this fails.

**Step 4 – Paste it into the system**

When an account needs verification its card shows a **"Paste verified Cookie" button** (粘贴无风控 Cookie). Click it to open the edit window. You can also go to Account Management → Edit account, paste the Cookie into the Cookie field and save.

Wait a moment; when the account turns online it is recovered.

## Pasted it and still fails? Check these

- **Did the web version really ask for verification?** If it never did, you may have copied the Cookie too early. Reload the Messages page a few times until verification shows up, pass it, then copy again. Some people only succeeded on the second copy, after a slider appeared on the web page.
- **Right place?** It must be Request Headers → Cookie of the `token` request.
- **Same network?** Toggling a VPN, switching proxy nodes or changing networks changes your IP and can invalidate the verification you just passed.
- **"WS Token is about to expire, rotating" and never recovers:** the Cookie in use is not a freshly verified one. Repeat the steps and copy right after passing verification.
- **The pasted Cookie disappears after saving:** the platform still rejects it and the system discarded it. Pass verification again and re-copy.
- **Wait a few hours.** Some accounts recovered by themselves hours later, and some people report that restarting the container helped. Neither is guaranteed.

## Order sync shows PERMISSION_EXCEPTION

It means the order-detail interface of this Cookie is flagged. On the web version go to "My Xianyu", open any product or order and pass any verification it shows. Then copy the Cookie again as above, paste it back into the account and re-run the sync. Upgrading to a recent version is also recommended.

## How to avoid it

- **Run it at home:** a NAS, soft router or an always-on home PC with Docker. Data-center and cloud IPs (including Hong Kong servers) are easy for the platform to recognise.
- **Keep your exit IP stable:** do not keep switching VPN or proxy nodes.
- **Do not use `-no-browser`:** verification and order-detail fallbacks need Chromium. Only turn the browser off if Chromium genuinely cannot be installed.
- **Run each account in one place only:** never sign the same account in on several machines.
- The program itself throttles requests to Xianyu (at least 2 seconds between requests per account, 5 seconds for background batches) and stops a batch immediately when risk control appears.

## Common misunderstandings

- "The API is broken": usually it is your network. Log lines such as `wsarecv ... forcibly closed` or `server misbehaving` mean this machine's network or DNS cannot reach Xianyu. Fix the network/DNS; the interface itself is fine.
- "A short network drop leaves the account offline forever": no. Newer versions reconnect after 5 s, 15 s, 30 s, 60 s and then every 2 minutes, and only stop when Xianyu explicitly rejects the login.

Never send your Cookie to anyone, paste it in a group chat or post it in an issue. It is equivalent to the account's login.

More answers in the [FAQ](FAQ). To check whether a failure is risk control, open [Runtime Logs](Runtime-Logs), set the source to Xianyu API and search `RGV587`.
