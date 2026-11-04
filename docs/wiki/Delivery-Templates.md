# Delivery Templates Guide

[中文](发货模板.zh-CN) | English

## Create a template

1. Open Delivery Templates and click New Delivery Template.
2. Enter a name and decide whether the template is enabled.
3. Enter one or more messages. They are sent separately in page order.
4. Click Add Message for additional messages. Unneeded messages can be removed, but a template must keep at least one message.
5. Click Save Template.

Each message is either text or an image: choosing an image from the image library below the text box turns the whole message into an image message (internal token `{{library_image:<imageID>}}`); it cannot be mixed with text, and clearing the image lets you type text again. See [Resource Library](Resource-Library).

## Available variables

Template messages can use these variables:

- `{{order_id}}`: order ID
- `{{buyer_id}}`: buyer ID
- `{{buyer_nickname}}`: buyer nickname (replaced with an empty string when missing)
- `{{card_name}}`: name of the card inventory bound to the rule
- `{{cards.<name>}}`: card content bound to the rule, for example `{{cards.main}}`
- `{{custom.<name>}}`: custom text entered in the automation rule

After `cards.` and `custom.`, names may only use English letters, digits, underscores or hyphens; no spaces or Chinese characters, and all braces must be present. The `delivery.` prefix is optional (`{{delivery.order_id}}` equals `{{order_id}}`). Other variables such as item ID or item title are not supported. The system validates variable spelling when saving. Template variables only apply to delivery templates.

## Use a template in automation

1. Save the template, then open Automation.
2. Select it in a Paid Delivery or Review Gift rule.
3. Bind card inventory to every `cards` variable used by the template.
4. Enter text for every `custom` variable required by the template.
5. Save and enable the rule, then use a test order to verify message order and substitution.

Delivery templates are only used for paid delivery and review gifts. They are not used for price changes or review reminders. A template referenced by a rule cannot be deleted; replace it in those rules first.
