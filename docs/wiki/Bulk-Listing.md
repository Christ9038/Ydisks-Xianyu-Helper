# Bulk Listing Guide

[中文](批量铺货.zh-CN) | English

Bulk listing runs preflight validation before starting a background publishing task. It handles at most 50 products per batch. Test with a one-row package before a production batch.

## 1. Prepare the package

The required file is a CSV, XLSX, or TSV table (an XLSX must contain exactly one worksheet; legacy `.xls` is not supported). Add a ZIP image package when the table references local images. Click “下载CSV模板” (Download CSV template) in Product Management → Bulk Listing to avoid header mistakes; that template omits the last optional column `求评价延迟秒`, so add it manually at the end of the header row when needed.

Recommended Chinese headers:

```text
账号ID,标题,描述,价格,原价,库存,邮费模式,邮费,图片,类目ID,类目名称,频道类目ID,淘宝类目ID,付款发货启用,付款发货内容,评价赠品启用,评价赠品内容,求评价启用,求评价等待小时,求评价文案,求评价最多次数,求评价延迟秒
```

English headers are also supported; avoid mixing Chinese and English headers. Header matching is case-insensitive and ignores spaces, underscores, and hyphens, and several aliases are accepted (see §2.1 of `docs/批量铺货使用文档.md`). Each row is one product. `账号ID` may be blank; the upload page’s “默认发布账号” (default publishing account) is then used.

### Required fields and formats

- Title is required and price must be greater than 0 (at most two decimals). Inventory is a positive integer; blank defaults to 1.
- Shipping mode defaults to free when blank. Accepted values: `free`/`包邮`, `fixed`/`固定邮费`/`一口价邮费` (enter the fee; it must not be negative — preflight only checks the format and non-negativity, and a blank fee is submitted as 0), `distance`/`按距离计费`, `none`/`无需邮寄`/`不需要邮寄`.
- Each product has 1–9 images. Separate multiple values with the English semicolon `;`. Use publicly accessible `http(s)://` URLs (preflight only checks the prefix; the download happens when that row is published, and a failed download fails the row) or relative paths inside the ZIP (preflight checks that the file exists).
- The upload-page default category may be blank. To specify one, select an account, enter a keyword, and use Xianyu’s category recommendation response. A row’s Category ID + Category Name + Channel Category ID overrides the current row; all three must be supplied together.
- Fill Taobao Category ID from the Xianyu recommendation response. Leave the value empty for Electronic Materials.
- Review-reminder delay is optional and should be an integer ≥ 0 (preflight does not validate this column); blank means send immediately.
- The local-image ZIP is limited to 200 MiB (at most 500 files and 500 MiB after extraction), each image to 10 MiB, and the table to 20 MiB.
- CSV files must use UTF-8; BOM is recommended.

### Automation columns

Paid-delivery content and review-gift content use the same format:

```text
卡密组ID:每件份数:延迟秒;卡密组ID:每件份数:延迟秒
```

For example, `101:1;102:2:30` sends one unit from group 101, waits 30 seconds, and sends two units from group 102. Omitted quantity defaults to 1 (must be greater than 0); omitted delay defaults to 0 (must be between 0 and 3600 seconds). Enabled values may be `是`, `true`, `1`, or `on` (also `yes`, `y`, `开启`, `启用`, case-insensitive); disabled values may be `否`, `false`, `0`, or blank, and anything else is treated as disabled. Once a product is published, the enabled automations create their rules (the paid-delivery rule always gets a confirm-shipment action appended after its delivery actions). See `docs/批量铺货使用文档.md` in the repository for the full field specification and examples.

## 2. Operate the task

1. Click Bulk Listing in Product Management (if a task is in progress it is restored when the dialog opens; after you close the dialog the button reads “继续批量任务”, Continue Batch Task).
2. Select the required "默认发布账号" (default publishing account); rows without an account ID use it, and a filled ID overrides it for that row only.
3. Leave the default category blank or enter a keyword and click Get Category to apply one to the batch.
4. Select the table and, when needed, the ZIP image package. Adjust "商品发布强制间隔" (forced publish interval, 1–3600 seconds, default 5) if needed: images are uploaded ahead of time and only the final publish requests wait at least that long between each other.
5. Click “开始预检” (Start Preflight). Check category strategy and fix errors such as an account that does not exist or does not belong to you, missing card group, mismatched image path, invalid fixed shipping fee format, or zero price. Each row shows “可发布” (publishable) or “需修正” (needs correction) with the reason in the problem column. Row numbers count non-empty data rows (the header is row 1); blank rows do not take a number.
6. Click “确认发布 N 个商品” (Confirm Publishing N Products) to start. The task can start as soon as at least one row is publishable; rows needing correction are not published and are not retried by Retry Failed, so fix the table and run preflight again (“返回修改” discards the current preflight). The task runs in the background; click Bulk Listing / Continue Batch Task again to restore an in-progress task, or “最近批次结果” (Latest Batch Result) after it finishes.
7. Review successful, failed, waiting, and in-progress rows in task details. Click “取消任务” (Cancel Task) when needed; a single product already being submitted may still finish, and the button then reads “正在保存远端结果并安全取消…”.
8. Use “重试失败项” (Retry Failed) for retryable rows (rows whose remote result is uncertain are marked “禁止自动重试，请人工核对闲鱼商品列表” and are not retried), and click “下载结果” (Download Result) after the task ends to get the result CSV for records.

### Category priority

Category priority is: row-specified category > upload-page default category fetched by keyword > automatic recognition from title, description, and images > Electronic Materials fallback. A user-specified category is never overwritten by automatic recognition. Timeouts, authentication failures, risk controls, and similar errors are reported rather than hidden by the final fallback.

Bulk listing targets virtual products: the optional "批次发货地" (batch location) may be left blank; if filled, the whole batch uses that one location, which is saved with the task for recovery and retry. Category selection only identifies the product type and does not change virtual delivery after payment.

## 3. Common issues

- **Image not found:** ZIP paths must exactly match the table, use `/`, not start with `/`, and contain no `..`.
- **Category not recognized:** the system uses Electronic Materials as the final fallback; timeout, authentication, and risk-control errors are still shown.
- **Where to get category IDs:** enter a keyword in the upload page and fetch the category; manual lookup of low-level IDs is unnecessary.
- **Automation has no effect:** check card-group ID, account ownership, enabled fields, and inventory. After publishing, confirm that the automation rule was created.
- **Task interrupted:** click Bulk Listing / Continue Batch Task or Latest Batch Result and confirm its status before creating another batch; retry failed rows only.
