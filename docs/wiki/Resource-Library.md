# Resource Library

[中文](资源库.zh-CN) | English

The **Resource Library** (`/app/library`, sidebar entry below Online Chat) stores reusable product templates and images, so the same product can be published to several accounts in one pass. It has two tabs: **Product templates** and **Image library**.

## Image library

- Upload images once and reuse them as main images or spec images. Each image is limited to **10 MB**; PNG / JPG / GIF / WEBP are supported, several files can be selected, and at most 20 per upload. Identical content is stored once; re-uploading shows it as already existing.
- Files are stored under the upload root (`XIANYU_UPLOAD_DIR`, subfolder `library`); no extra Compose change is needed.
- Search by file name, preview a larger image, and delete single or multiple images.
- A badge shows how many product templates reference an image. The single-product publish dialog can also pick images from this library.
- Tick images to delete them with "Delete selected"; "Select all results" and "Clear selection" are available.

The image library can be filtered by format (JPG / PNG / WEBP / GIF) and usage (referenced by templates / not referenced), and sorted by newest, oldest, file name A→Z / Z→A, size large→small / small→large. Use "Reset filters" to clear them; images still referenced by a product template, keyword reply, account default reply, item first-reply rule, image card, quick reply, or delivery template cannot be deleted (HTTP 409; the message lists where they are used; remove the references first). The picker can also filter by format and sort.

Library images can be chosen directly when publishing items and editing product templates, in delivery-template image messages, image-type cards, keyword/default replies, item first-reply messages, and when sending images or quick-reply images in online chat.

## Product templates

A template holds everything needed to publish a product:

- Template name, product title, description, price, stock, original price (optional) and postage (free / by distance / fixed / no shipping needed); the category is fixed to digital materials (电子资料) and the ship-from location uses each account's platform default.
- **1-9 main images** (from the image library; the first one is the cover; reorder with the arrows) and optional images for each spec value.
- Multi-spec configuration (SKU combinations with price and stock).
- **Post-publish automation:** paid-delivery rules (card stock or delivery template, one per full spec combination), review-gift rules and review-request settings (hours after shipment, maximum sends, delay seconds, message). Multi-spec products must configure every spec combination.

The template list can be searched by template name, title, description or category, filtered by spec (single / multi), paid delivery (enabled / not enabled) and postage mode (free / by distance / fixed / no shipping needed), and sorted by recently updated, oldest updated, name A→Z, price low→high / high→low, stock high→low. Each template card has Edit, Duplicate and Delete; use "Select all results", or tick templates and click "Publish selected" or "Delete selected" (already published items are unaffected).

## Publishing templates to accounts

1. Tick one or more templates and click "Publish selected".
2. Choose target accounts (only available accounts are listed) and the publish interval (seconds, 0–3600; 0 uses the default of 5).
3. The system builds a **template × account preview**. Invalid rows show their reason and can be filtered with "Only show rows to fix" (只看需修正).
4. Confirm. Publishing then follows the [Bulk Listing](Bulk-Listing) pipeline: serial publishing with the enforced interval, progress display and failed-row retry. The total rows per batch (templates × accounts) are capped by the same limit as bulk listing.

Automation rules defined on the template are written for each published item using its full spec combination.
