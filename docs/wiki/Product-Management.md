# Product Management Guide

[中文](商品管理.zh-CN) | English

## Sync existing products

1. Open Product List in the sidebar; the page title is Product Management.
2. Select the accounts to sync in Sync Product Accounts.
3. Click Sync Products and wait for completion.
4. Use the product-list filter by account and confirm titles, prices, images, and product IDs.

“Link Delivery Rule” means no usable rule is currently associated with the product. Products with rules show “View Delivery Rules”.

## Add a product manually

Add Product creates a local association for a product that already exists on Xianyu but is not recorded in the system. Enter the owning account, Xianyu product ID, price, title, and image URLs, then save and configure automation.

## Publish one product

1. Click Publish Product and select the publishing account.
2. Enter title, inventory, description, price, original price, and shipping fee. A virtual product may omit the shipping origin.
3. Optionally enter a category keyword and fetch a category. If blank, Xianyu attempts automatic recognition, then uses Electronic Materials as the fallback.
4. Add specifications when needed. Up to two specification types are supported, with at least two values per type.
5. Fill price and inventory for each generated combination; upload specification images when needed.
6. Upload product images. Up to nine are supported; the first is the main image. Images can be added, removed, or reordered.
7. Recheck account, category, inventory, and images, then click Publish to Xianyu.

Single-specification products use one total inventory value. Multi-specification inventory is the sum of combination inventory. After publishing, associate paid-delivery, review-gift, or review-reminder rules in Automation.

### Postage mode

A single listing offers four modes: **free shipping**, **by distance**, **fixed postage** (enter the amount) and **no shipping needed** (typical for virtual goods). Product templates and bulk listing support the same modes; see [Bulk Listing](Bulk-Listing) for the table values.

## Edit and delete

- Edit on a product card changes title, price, category, or description.
- AI Customer Service on a product card sets whether AI replies for that product (Inherit Account, Force Enable, or Force Disable) and stores product-specific reference material for AI.
- Delete removes only the local record; it does not delete the product on Xianyu. Confirm that orders and rules will not be affected.
- Use Link Delivery Rule/View Delivery Rules to inspect the product rules. For specification products, verify every specification match.

## Bulk listing

See [Bulk Listing](Bulk-Listing). A bulk task runs preflight validation first and only starts after all rows marked for correction are fixed. Its automatic-delivery columns reference card-group IDs from Card Inventory.
