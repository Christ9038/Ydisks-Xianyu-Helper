#!/bin/sh
set -eu

test_instance() {
  name="$1"
  base_url="$2"
  cookie_file="/tmp/${name}.cookies"

  curl -fsS "${base_url}/health" | grep -q '"database":"ok"'
  curl -fsS "${base_url}/verify" | grep -q '"initialized":true'
  curl -fsS -c "$cookie_file" -H 'Content-Type: application/json' \
    -d '{"username":"docker_fixture","password":"docker_fixture_password"}' \
    "${base_url}/login" | grep -q '"success":true'
  curl -fsS -b "$cookie_file" "${base_url}/dashboard/stats" | grep -q '"total_cookies":1'
  curl -fsS -b "$cookie_file" "${base_url}/analytics/orders?timezone_offset_minutes=480" | grep -q '"revenue_stats"'
  curl -fsS -b "$cookie_file" "${base_url}/cards" | grep -q '\[Docker测试\]'
  curl -fsS -b "$cookie_file" "${base_url}/api/orders?search=docker&page=1&page_size=5" | grep -q '"success":true'
  curl -fsS -b "$cookie_file" "${base_url}/items/publish-batches?limit=10" | grep -q '"batches"'
  curl -fsS -b "$cookie_file" "${base_url}/automation-issues" | grep -q '"runs"'

  invalid_amount_status="$(curl -sS -o /tmp/${name}-invalid-amount.json -w '%{http_code}' -b "$cookie_file" \
    -H 'Content-Type: application/json' -X PUT -d '{"amount":"abc"}' \
    "${base_url}/api/orders/docker-invalid-amount")"
  test "$invalid_amount_status" = "400"
  curl -fsS -b "$cookie_file" -H 'Content-Type: application/json' -X PUT \
    -d '{"pause_duration":1}' "${base_url}/cookies/docker-fixture-account/pause-duration" | grep -q '"paused":true'
  curl -fsS -b "$cookie_file" "${base_url}/cookies/docker-fixture-account/pause-duration" | grep -q '"paused":true'

  keyword_response="$(curl -fsS -b "$cookie_file" -H 'Content-Type: application/json' \
    -d '{"keyword":"docker-keyword-'"$name"'","reply":"before","item_id":"","type":"text","image_url":""}' \
    "${base_url}/keywords-with-item-id/docker-fixture-account")"
  keyword_id="$(printf '%s' "$keyword_response" | sed -n 's/.*"id":\([0-9][0-9]*\).*/\1/p')"
  test -n "$keyword_id"
  curl -fsS -b "$cookie_file" -H 'Content-Type: application/json' -X PUT \
    -d '{"keyword":"docker-keyword-'"$name"'","reply":"after","item_id":"","type":"text","image_url":""}' \
    "${base_url}/keywords-with-type/docker-fixture-account/${keyword_id}" | grep -q '"success":true'
  # 完全相同的更新在 MySQL 也必须成功；用于覆盖 clientFoundRows 的匹配行语义。
  curl -fsS -b "$cookie_file" -H 'Content-Type: application/json' -X PUT \
    -d '{"keyword":"docker-keyword-'"$name"'","reply":"after","item_id":"","type":"text","image_url":""}' \
    "${base_url}/keywords-with-type/docker-fixture-account/${keyword_id}" | grep -q '"success":true'
  curl -fsS -b "$cookie_file" "${base_url}/keywords-with-type/docker-fixture-account" | grep -q '"reply":"after"'
  curl -fsS -b "$cookie_file" -X DELETE \
    "${base_url}/keywords-with-type/docker-fixture-account/${keyword_id}" | grep -q '"success":true'

  admin_status="$(curl -sS -o /tmp/${name}-admin.json -w '%{http_code}' -b "$cookie_file" "${base_url}/admin/stats")"
  test "$admin_status" = "403"

  admin_cookie_file="/tmp/${name}.admin.cookies"
  curl -fsS -c "$admin_cookie_file" -H 'Content-Type: application/json' \
    -d '{"username":"docker_admin","password":"docker_admin_password"}' \
    "${base_url}/login" | grep -q '"success":true'
  curl -fsS -b "$admin_cookie_file" -H 'Content-Type: application/json' -X PUT \
    -d '{"theme_color":"docker-blue","renewal_log_retention_days":15}' \
    "${base_url}/system-settings" | grep -q '"success":true'
  curl -fsS -b "$admin_cookie_file" "${base_url}/system-settings" | grep -q '"theme_color":"docker-blue"'
  invalid_bulk_status="$(curl -sS -o /tmp/${name}-invalid-settings.json -w '%{http_code}' -b "$admin_cookie_file" \
    -H 'Content-Type: application/json' -X PUT -d '{"theme_color":"must-not-save","log_level":"verbose"}' \
    "${base_url}/system-settings")"
  test "$invalid_bulk_status" = "400"
  curl -fsS -b "$admin_cookie_file" "${base_url}/system-settings" | grep -q '"theme_color":"docker-blue"'
  printf '%s functional test passed\n' "$name"
}

test_instance sqlite http://app-sqlite:8080
test_instance mysql http://app-mysql:8080
test_instance postgres http://app-postgres:8080
