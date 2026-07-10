#!/bin/sh
set -eu

test_instance() {
  name="$1"
  base_url="$2"
  cookie_file="/tmp/${name}.cookies"

  curl -fsS "${base_url}/health" | grep -q '"status":"ok"'
  curl -fsS "${base_url}/verify" | grep -q '"initialized":true'
  curl -fsS -c "$cookie_file" -H 'Content-Type: application/json' \
    -d '{"username":"docker_fixture","password":"docker_fixture_password"}' \
    "${base_url}/login" | grep -q '"success":true'
  curl -fsS -b "$cookie_file" "${base_url}/dashboard/stats" | grep -q '"total_cookies":1'
  curl -fsS -b "$cookie_file" "${base_url}/analytics/orders" | grep -q '"revenue_stats"'
  curl -fsS -b "$cookie_file" "${base_url}/cards" | grep -q '\[Docker测试\]'

  admin_status="$(curl -sS -o /tmp/${name}-admin.json -w '%{http_code}' -b "$cookie_file" "${base_url}/admin/stats")"
  test "$admin_status" = "403"
  printf '%s functional test passed\n' "$name"
}

test_instance mysql http://app-mysql:8080
test_instance postgres http://app-postgres:8080
