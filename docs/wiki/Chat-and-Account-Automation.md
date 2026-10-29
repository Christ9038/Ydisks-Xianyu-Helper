# Chat and Account Automation Guide

[中文](在线聊天与账号自动任务.zh-CN) | English

## Online chat

Open Chat to see an account tab at the top. Each account has an isolated conversation list and message state. The lower area uses a standard two-column layout: conversations on the left and the current message window on the right.

Chat supports:

- Contact, avatar, nickname, user ID, product summary, and unread count; conversations are ordered by the last Xianyu message.
- Local cache is read first when opening a conversation, then the Xianyu history API fills missing messages. Older messages and more historical contacts can be loaded at the bottom.
- Text, Xianyu text emojis, and image sending. Video sending from the management console is not supported, so no video button is shown.
- Sender labels for the other party and the account owner, with corresponding avatars. Conversations and live messages are isolated between accounts.
- Real-time events through `/api/v1/chat/ws`. After refresh or reconnect, history API data and the local database remain authoritative; the UI does not rely only on messages received while a WebSocket was open.

### Conversation actions

- **Send item**: pick an item card (switch between the buyer's items and your account's items, with search) and send it straight into the chat.
- **Message status**: your messages show sending, failed (with retry), sent-unread and read by the other side.
- **Delete conversation**: clears the chat messages with that buyer from this machine and hides the conversation. Orders, automation and AI memory are untouched, and the conversation reappears when the buyer writes again.
- The search box finds users, items or message text; paste or choose images to send (up to 10 MB).

### Official-message recognition

Transaction cards and platform notices must not be handled as ordinary buyer messages. The current protocol metadata rules are:

- `contentType=14`: platform notice;
- `contentType=25`: review reminder after confirmation of receipt;
- `contentType=26`: transaction card;
- sender user ID `1400`: Xianyu’s assistant;
- “new message received”: official-notice placeholder content;
- “Please give them a review~” or its tilde variant: review-reminder summary.

Recognized messages do not enter keyword, default, or AI reply chains. They appear as centered system cards. Empty conversation shells returned by contact pagination are not imported, preventing contacts that only show “No messages yet”.

If a nickname or avatar is missing, click refresh in the conversation list. The system assistant uses its platform avatar and fixed name; do not treat a transaction-card title as a buyer nickname.

## Automatic reviews

Go to Account Management → Automatic Reviews and Daily Refresh. This is an account-level switch, not separate copy per buyer:

1. Enable Automatic Reviews.
2. Enter one uniform positive-review message, up to 500 characters.
3. After saving, the backend continuously scans pending-review orders.
4. Each order is processed idempotently by transaction order ID; handled orders are not reviewed twice.

This is not a daily task. Use Review Now for a one-time scan or rely on the continuous scanner. It does not run when the account is disabled or paused.

## Daily listing refresh

Daily Refresh is also account-level but differs from automatic reviews:

- Execution time uses Beijing Time.
- Each account runs at most once per day.
- The default time is `03:00`; adjust it in `HH:mm` format.
- The backend reads the product list and refreshes products one by one, currently up to 20 products per page and 20 pages.
- Refresh Now triggers a manual run, but idempotency skips it when that account has already succeeded today.
- The page shows the latest completion time, discovered count, successes, failures, and skips.

Partial failures record their reasons and become retryable. Do not infer that every product succeeded from the button response; inspect the run result.

## Boundary with transaction automation

Automatic Reviews/Daily Refresh handles account-level tasks. Automation handles order and chat triggers:

- paid delivery;
- price change for unpaid orders;
- review gifts;
- review reminders;
- keyword replies, default replies, and AI replies;
- delivery templates, which are limited to paid delivery and review gifts.

Official system messages are for platform-event recognition only and should not be configured as ordinary buyer keyword triggers.

## Quick replies and buyer notes

- **Quick replies:** saved per account (up to 50). Click one in the chat side panel to send it through the normal reliable-send path; your unsent draft is kept. Add or delete them from the same panel (deleting asks for confirmation).
- **Buyer notes:** when chatting with a confirmed buyer, the chat header shows the note (or "Add user note"). Click it to open a dialog to read and edit the full note. The entry is hidden when the other party is a seller.
- **Unread alert:** a global unread indicator and browser-title flashing notify you of new messages; browser notifications play a sound by default, switchable under System Settings → Basic Settings.
