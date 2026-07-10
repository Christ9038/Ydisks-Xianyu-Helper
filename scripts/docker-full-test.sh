#!/bin/sh
set -eu

compose="docker compose -f docker-compose.functional.yml"

$compose up -d --build mysql postgres
$compose build seed-mysql frontend-test go-vet go-lint go-test
$compose run --rm frontend-test
$compose run --rm go-vet
$compose run --rm go-lint
$compose run --rm go-test
$compose run --rm dbverify-mysql
$compose run --rm dbverify-postgres
$compose up -d --build app-mysql app-postgres
$compose run --rm --no-deps functional-test

mysql_before="$($compose exec -T mysql mysql -uroot -pxianyu_root_password -Nse "SELECT CONCAT((SELECT COUNT(*) FROM xianyu.users WHERE username='docker_fixture'), ':', (SELECT COUNT(*) FROM xianyu.cards c JOIN xianyu.users u ON u.id=c.user_id WHERE u.username='docker_fixture'), ':', (SELECT COUNT(*) FROM xianyu.orders o JOIN xianyu.cookies c ON c.id=o.cookie_id JOIN xianyu.users u ON u.id=c.user_id WHERE u.username='docker_fixture'))")"
postgres_before="$($compose exec -T postgres psql -U xianyu -d xianyu -Atc "SELECT (SELECT COUNT(*) FROM users WHERE username='docker_fixture') || ':' || (SELECT COUNT(*) FROM cards c JOIN users u ON u.id=c.user_id WHERE u.username='docker_fixture') || ':' || (SELECT COUNT(*) FROM orders o JOIN cookies c ON c.id=o.cookie_id JOIN users u ON u.id=c.user_id WHERE u.username='docker_fixture')")"

$compose restart mysql postgres
$compose up -d --wait mysql postgres
$compose restart app-mysql app-postgres
$compose up -d --wait --no-deps app-mysql app-postgres

mysql_after="$($compose exec -T mysql mysql -uroot -pxianyu_root_password -Nse "SELECT CONCAT((SELECT COUNT(*) FROM xianyu.users WHERE username='docker_fixture'), ':', (SELECT COUNT(*) FROM xianyu.cards c JOIN xianyu.users u ON u.id=c.user_id WHERE u.username='docker_fixture'), ':', (SELECT COUNT(*) FROM xianyu.orders o JOIN xianyu.cookies c ON c.id=o.cookie_id JOIN xianyu.users u ON u.id=c.user_id WHERE u.username='docker_fixture'))")"
postgres_after="$($compose exec -T postgres psql -U xianyu -d xianyu -Atc "SELECT (SELECT COUNT(*) FROM users WHERE username='docker_fixture') || ':' || (SELECT COUNT(*) FROM cards c JOIN users u ON u.id=c.user_id WHERE u.username='docker_fixture') || ':' || (SELECT COUNT(*) FROM orders o JOIN cookies c ON c.id=o.cookie_id JOIN users u ON u.id=c.user_id WHERE u.username='docker_fixture')")"

test "${mysql_before%%:*}" = "1"
test "${postgres_before%%:*}" = "1"
test "$mysql_after" = "$mysql_before"
test "$postgres_after" = "$postgres_before"

$compose run --rm --no-deps functional-test

printf 'MySQL 8 and PostgreSQL 17 persistent-volume tests passed\n'
