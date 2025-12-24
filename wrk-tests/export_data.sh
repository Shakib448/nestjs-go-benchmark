#!/bin/bash
# Export test data in simple text format for wrk scripts

set -e

DB_HOST="${DB_HOST:-localhost}"
DB_PORT="${DB_PORT:-5434}"
DB_NAME="${DB_NAME:-loadtest}"
DB_USER="${DB_USER:-postgres}"
DB_PASSWORD="${DB_PASSWORD:-postgres}"
OUTPUT_DIR="${OUTPUT_DIR:-./data}"

export PGPASSWORD="$DB_PASSWORD"

echo "📦 Exporting test data for wrk..."
mkdir -p "$OUTPUT_DIR"

# Export user IDs (one per line)
echo "   Exporting user IDs..."
psql -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$DB_NAME" -t -A -c "
    SELECT id FROM users WHERE status = 'active' ORDER BY RANDOM() LIMIT 1000;
" > "$OUTPUT_DIR/user_ids.txt"

# Export product IDs (one per line)
echo "   Exporting product IDs..."
psql -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$DB_NAME" -t -A -c "
    SELECT id FROM products WHERE status = 'active' ORDER BY RANDOM() LIMIT 500;
" > "$OUTPUT_DIR/product_ids.txt"

echo ""
echo "✅ Test data exported to $OUTPUT_DIR/"
wc -l "$OUTPUT_DIR"/*.txt
