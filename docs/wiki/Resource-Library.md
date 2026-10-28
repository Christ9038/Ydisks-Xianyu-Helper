# Resource Library

[中文](资源库.zh-CN) | English

The **Resource Library** (`/app/library`, sidebar entry below Online Chat) stores reusable product templates and images, so the same product can be published to several accounts in one pass. It has two tabs: **Product templates** and **Image library**.

## Image library

- Upload images once and reuse them as main images or spec images. Each image is limited to **10 MB**.
- Files are stored under the upload root (`XIANYU_UPLOAD_DIR`, subfolder `library`); no extra Compose change is needed.
- Search by file name, preview a larger image, and delete single or multiple images.
- A badge shows how many templates reference an image. The single-product publish dialog can also pick images from this library.

## Product templates

A template holds everything needed to publish a product:

- Template name, product title, description, price, original price (optional) and postage (free / fixed).
- Up to **9 main images** (first one is the cover; reorder with the arrows) and optional images for each spec value.
- Multi-spec configuration (SKU combinations with price and stock).
- **Post-publish automation:** paid-delivery rules (card stock or delivery template, one per full spec combination), review-gift rules and review-request settings (hours after shipment, maximum sends, delay seconds, message). Multi-spec products must configure every spec combination.

## Publishing templates to accounts

1. Tick one or more templates and click publish.
2. Choose target accounts and the publish interval (seconds, 0–3600).
3. The system builds a **template × account preview**. Invalid rows show their reason and can be filtered.
4. Confirm. Publishing then follows the [Bulk Listing](Bulk-Listing) pipeline: serial publishing with the enforced interval, progress display and failed-row retry. The total rows per batch (templates × accounts) are capped by the same limit as bulk listing.

Automation rules defined on the template are written for each published item using its full spec combination.
