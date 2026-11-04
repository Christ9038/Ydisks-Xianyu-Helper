# Product Management Guide

[中文](商品管理.zh-CN) | English

## Sync existing products

1. Open Product List in the sidebar; the page title is Product Management.
2. Select the accounts to sync in Sync Product Accounts.
3. Click Sync Products and wait for completion.
4. Use the product-list filter by account and confirm titles, prices, images, and product IDs.

The page also offers an automation-rule filter (to find products with no trade automation at all, or missing one of paid delivery, review gift, review reminder, or order-created price change) and a display mode (card or list; the list mode shows 20 products per page with the rule count for each of the four trade automations and the AI customer-service state).

The button at the bottom of a product card reads “Link Delivery Rule” when the product has no associated rule at all, “View Delivery Rules” when it has rules but no paid-delivery rule, and “N delivery rules” when it has paid-delivery rules. The card also shows “N rules” or “Not configured”.

## Add a product manually

Add Product creates a local association for a product that already exists on Xianyu but is not recorded in the system. Select the owning account and enter the Xianyu product ID (both required), optionally fill price, title, and image URL, then save and configure automation.

## Publish one product

1. Click Publish Product and select the publishing account.
2. Enter title, inventory, description (the title is used when blank), price, original price (optional), and postage mode. The optional shipping origin may be omitted for a virtual product.
3. Optionally enter a category keyword and fetch a category. If blank, Xianyu attempts automatic recognition, then uses Electronic Materials as the fallback.
4. Add specifications when needed. Up to two specification types are supported, with at least two values per type, and at most 1500 combinations in total.
5. Fill price and inventory for each generated combination; upload specification images when needed.
6. Upload product images. Up to nine are supported; the first is the main image. Images can be added, removed, or reordered.
7. Recheck account, category, inventory, and images, then click Publish to Xianyu. After a successful publish, the product’s delivery-rule configuration opens automatically.

Single-specification products use one total inventory value. Multi-specification inventory is the sum of combination inventory. After publishing, associate paid-delivery, review-gift, or review-reminder rules in Automation.

### Postage mode

A single listing offers four modes: **free shipping**, **by distance**, **fixed postage** (enter an amount greater than 0) and **no shipping needed** (typical for virtual goods). Product templates and bulk listing support the same modes; see [Bulk Listing](Bulk-Listing) for the table values.

## Edit and delete

- Edit on a product card changes title, price, category, or description.
- AI Customer Service on a product card sets whether AI replies for that product (Inherit Account, Force Enable, or Force Disable) and stores product-specific reference material for AI.
- Delete (the trash icon shown on hover on a card, or in the actions column in list mode) removes only the local record; it does not delete the product on Xianyu. A confirmation prompt appears; confirm that orders and rules will not be affected.
- Use Link Delivery Rule/View Delivery Rules/N delivery rules to inspect the product rules. For specification products, verify every specification match.

## Bulk listing

See [Bulk Listing](Bulk-Listing). A bulk task runs preflight validation first; each row is marked publishable or needing correction. After start only publishable rows are published, and rows needing correction are not published, so fix the table and run preflight again. Its automatic-delivery columns reference card-group IDs from Card Inventory.
