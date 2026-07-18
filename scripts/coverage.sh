#!/bin/bash
# run-coverage.sh — 完整覆盖率报告生成（Web + Go）
# 用于 CI artifact 或本地检查
set -e
echo "=== Web Coverage ==="
cd web
pnpm test:coverage:html 2>&1 | tail -20
echo ""
echo "=== Go Coverage ==="
cd ..
go test -coverprofile=/tmp/cover.out ./...
go tool cover -html=/tmp/cover.out -o ./go-coverage.html
go tool cover -func=/tmp/cover.out | tail -3
echo ""
echo "=== 报告位置 ==="
echo "  Web HTML: $(pwd)/web/coverage/index.html"
echo "  Go HTML:  $(pwd)/go-coverage.html"
