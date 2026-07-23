#!/bin/sh
set -eu

compose="docker compose -f docker-compose.functional.yml"

$compose up -d --build mysql postgres
$compose build seed-sqlite seed-mysql frontend-test go-vet go-lint go-test browser-integration-test
$compose run --rm frontend-test
$compose run --rm go-vet
$compose run --rm go-lint
$compose run --rm go-test
$compose run --rm browser-integration-test
$compose run --rm dbverify-sqlite
$compose run --rm dbverify-mysql
$compose run --rm dbverify-postgres
$compose rm -sf sqlite-seed-copy seed-sqlite seed-mysql seed-postgres
$compose up --build sqlite-seed-copy
$compose up --build seed-sqlite seed-mysql seed-postgres
$compose up -d --build app-sqlite app-mysql app-postgres
$compose run --rm --no-deps functional-test

./scripts/docker-persistence-test.sh
