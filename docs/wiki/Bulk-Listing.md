# Bulk Listing Guide

[中文](批量铺货.zh-CN) | English

Bulk listing runs preflight validation before starting a background publishing task. It handles at most 50 products per batch. Test with a one-row package before a production batch.

## 1. Prepare the package

The required file is a CSV, XLSX, or TSV table. Add a ZIP image package when the table references local images. Download the template from Product Management → Bulk Listing to avoid header mistakes.

Recommended Chinese headers:

```text
账号ID,标题,描述,价格,原价,库存,邮费模式,邮费,图片,类目ID,类目名称,频道类目ID,淘宝类目ID,付款发货启用,付款发货内容,评价赠品启用,评价赠品内容,求评价启用,求评价等待小时,求评价文案,求评价最多次数,求评价延迟秒
```

English headers are also supported, but Chinese and English headers cannot be mixed. Each row is one product. `账号ID` may be blank; the upload page’s default account is then used.

### Required fields and formats

- Title is required and price must be greater than 0. Inventory is a positive integer; blank defaults to 1.
- Shipping mode defaults to free when blank. Accepted values: `free`/`包邮`, `fixed`/`固定邮费`/`一口价邮费` (a non-negative fee is required), `distance`/`按距离计费`, `none`/`无需邮寄`/`不需要邮寄`.
- Each product has 1–9 images. Separate multiple values with the English semicolon `;`. Use publicly accessible `http(s)://` URLs or relative paths inside the ZIP.
- The upload-page default category may be blank. To specify one, select an account, enter a keyword, and use Xianyu’s category recommendation response. A row’s Category ID + Category Name + Channel Category ID overrides the current row; all three must be supplied together.
- Fill Taobao Category ID from the Xianyu recommendation response. Leave the value empty for Electronic Materials.
- Review-reminder delay is optional and must be an integer ≥ 0; blank means send immediately.
- The local-image ZIP is limited to 200 MiB, each image to 10 MiB, and the table to 20 MiB.
- CSV files must use UTF-8; BOM is recommended. Do not add spaces to headers.

### Automation columns

Paid-delivery content and review-gift content use the same format:

```text
卡密组ID:每件份数:延迟秒;卡密组ID:每件份数:延迟秒
```

For example, `101:1;102:2:30` sends one unit from group 101, waits 30 seconds, and sends two units from group 102. Omitted quantity defaults to 1; omitted delay defaults to 0. Enabled values may be `是`, `true`, `1`, or `on`; disabled values may be `否`, `false`, `0`, or blank. See `docs/批量铺货使用文档.md` in the repository for the full field specification and examples.

## 2. Operate the task

1. Click Bulk Listing in Product Management.
2. Select the required "默认发布账号" (default publishing account); rows without an account ID use it, and a filled ID overrides it for that row only.
3. Leave the default category blank or enter a keyword and click Get Category to apply one to the batch.
4. Select the table and, when needed, the ZIP image package. Adjust "商品发布强制间隔" (forced publish interval, 1–3600 seconds, default 5) if needed: images are uploaded ahead of time and only the final publish requests wait at least that long between each other.
5. Run preflight. Check category strategy and fix errors such as an unauthorized account, missing card group, mismatched image path, missing fixed shipping fee, or zero price.
6. Start the task only after every publishable row is confirmed.
7. Review successful, failed, and in-progress rows in task details. Cancel when needed; a single product already being submitted may still finish.
8. Use Retry Failed for retryable rows and download the result CSV for records.

### Category priority

Category priority is: row-specified category > upload-page default category fetched by keyword > automatic recognition from title, description, and images > Electronic Materials fallback. A user-specified category is never overwritten by automatic recognition. Timeouts, authentication failures, risk controls, and similar errors are reported rather than hidden by the final fallback.

Bulk listing targets virtual products: the optional "批次发货地" (batch location) may be left blank; if filled, the whole batch uses that one location, which is saved with the task for recovery and retry. Category selection only identifies the product type and does not change virtual delivery after payment.

## 3. Common issues

- **Image not found:** ZIP paths must exactly match the table, use `/`, not start with `/`, and contain no `..`.
- **Category not recognized:** the system uses Electronic Materials as the final fallback; timeout, authentication, and risk-control errors are still shown.
- **Where to get category IDs:** enter a keyword in the upload page and fetch the category; manual lookup of low-level IDs is unnecessary.
- **Automation has no effect:** check card-group ID, account ownership, enabled fields, and inventory. After publishing, confirm that the automation rule was created.
- **Task interrupted:** reopen batch details and confirm its status before creating another batch; retry failed rows only.
