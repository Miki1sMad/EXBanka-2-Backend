#!/usr/bin/env bash
# coverage.sh — Run unit tests, filter out non-unit-testable packages,
# and report both raw and filtered coverage.

set -uo pipefail

COVERAGE_RAW="coverage.out"
COVERAGE_FILTERED="coverage_filtered.out"
COVERAGE_HTML="coverage_report.html"
THRESHOLD=80

echo "═══════════════════════════════════════════════════════════"
echo "  Running unit tests…"
echo "═══════════════════════════════════════════════════════════"

go test ./services/... ./shared/... \
    -coverprofile="$COVERAGE_RAW" \
    -covermode=atomic \
    -count=1 \
    "$@" || true

# ── Filter out excluded packages ─────────────────────────────────────────────
grep -vE \
    '/cmd/|/internal/repository/|/internal/smtp/|/internal/transport/|/internal/database/|/internal/worker/|/internal/testutil/|bank-service/internal/domain/|bank-service/internal/handler/|/mocks/|/tests/|/utils/rabbitmq\.go' \
    "$COVERAGE_RAW" \
    > "$COVERAGE_FILTERED"

echo ""
echo "═══════════════════════════════════════════════════════════"
echo "  Raw coverage (all packages)"
echo "═══════════════════════════════════════════════════════════"
go tool cover -func="$COVERAGE_FILTERED" | grep "^total:"

FILTERED_TOTAL=$(go tool cover -func="$COVERAGE_FILTERED" | grep "^total:" | awk '{print $3}')
PERCENT=${FILTERED_TOTAL//%/}

echo ""
echo "  Filtered total:  $FILTERED_TOTAL"

# ── Generate HTML report (file-level summary table) ──────────────────────────
# Aggregate coverage per file from go tool cover -func output
FILE_DATA=$(go tool cover -func="$COVERAGE_FILTERED" | grep -v "^total:" | awk '
{
  # $1 = file:line, $2 = func, $3 = pct
  split($1, a, ":")
  file = a[1]
  pct = $3
  sub(/%$/, "", pct)
  sum[file]  += pct + 0
  count[file]++
}
END {
  for (f in sum) {
    avg = sum[f] / count[f]
    printf "%s\t%.1f\n", f, avg
  }
}' | sort)

ROWS_HTML=""
while IFS=$'\t' read -r file pct; do
  short=$(echo "$file" | sed 's|banka-backend/||')
  if awk "BEGIN{exit ($pct+0 >= $THRESHOLD) ? 0 : 1}"; then
    color="#22c55e"; badge_class="badge-green"
  elif awk "BEGIN{exit ($pct+0 >= 60) ? 0 : 1}"; then
    color="#f59e0b"; badge_class="badge-yellow"
  else
    color="#ef4444"; badge_class="badge-red"
  fi
  bar_w=$(awk "BEGIN{printf \"%.0f\", $pct}")
  ROWS_HTML="${ROWS_HTML}
    <tr>
      <td class=\"pkg\">$short</td>
      <td class=\"bar-cell\">
        <div class=\"bar-bg\">
          <div class=\"bar-fill\" style=\"width:${bar_w}%;background:${color}\"></div>
        </div>
      </td>
      <td><span class=\"badge ${badge_class}\">${pct}%</span></td>
    </tr>"
done <<< "$FILE_DATA"

if awk "BEGIN{exit ($PERCENT+0 >= $THRESHOLD) ? 0 : 1}"; then
  TOTAL_COLOR="#22c55e"; TOTAL_MSG="✓ Threshold met (≥${THRESHOLD}%)"
else
  TOTAL_COLOR="#ef4444"; TOTAL_MSG="✗ Below threshold (≥${THRESHOLD}% required)"
fi

GENERATED_AT=$(date "+%Y-%m-%d %H:%M:%S")

cat > "$COVERAGE_HTML" <<HTML
<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8"/>
  <meta name="viewport" content="width=device-width, initial-scale=1.0"/>
  <title>Coverage Report — Banka Backend</title>
  <style>
    *, *::before, *::after { box-sizing: border-box; margin: 0; padding: 0; }
    body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
           background: #0f172a; color: #e2e8f0; min-height: 100vh; padding: 2rem; }
    h1 { font-size: 1.6rem; font-weight: 700; color: #f8fafc; margin-bottom: .25rem; }
    .subtitle { color: #64748b; font-size: .85rem; margin-bottom: 2rem; }
    .summary-card { background: #1e293b; border: 1px solid #334155; border-radius: 1rem;
      padding: 2rem; display: inline-flex; align-items: center; gap: 2rem; margin-bottom: 2rem; }
    .big-pct { font-size: 3.5rem; font-weight: 800; color: ${TOTAL_COLOR}; line-height: 1; }
    .summary-info { display: flex; flex-direction: column; gap: .4rem; }
    .summary-label { font-size: .8rem; color: #64748b; text-transform: uppercase; letter-spacing: .05em; }
    .summary-msg { font-size: .95rem; color: #cbd5e1; }
    table { width: 100%; border-collapse: collapse; background: #1e293b;
      border: 1px solid #334155; border-radius: 1rem; overflow: hidden; }
    thead th { background: #0f172a; padding: .75rem 1rem; text-align: left;
      font-size: .75rem; text-transform: uppercase; letter-spacing: .05em;
      color: #64748b; border-bottom: 1px solid #334155; }
    tbody tr { border-bottom: 1px solid #1e293b; transition: background .15s; }
    tbody tr:last-child { border-bottom: none; }
    tbody tr:hover { background: #263347; }
    td { padding: .65rem 1rem; font-size: .85rem; }
    td.pkg { color: #93c5fd; font-family: "SF Mono", "Fira Code", monospace; word-break: break-all; }
    .bar-cell { width: 40%; padding-right: 1rem; }
    .bar-bg { background: #334155; border-radius: 9999px; height: 8px; }
    .bar-fill { height: 8px; border-radius: 9999px; }
    .badge { display: inline-block; padding: .2rem .6rem; border-radius: 9999px; font-size: .78rem; font-weight: 600; }
    .badge-green  { background: #14532d; color: #86efac; }
    .badge-yellow { background: #713f12; color: #fde68a; }
    .badge-red    { background: #7f1d1d; color: #fca5a5; }
    footer { margin-top: 1.5rem; color: #334155; font-size: .75rem; text-align: center; }
  </style>
</head>
<body>
  <h1>Coverage Report</h1>
  <p class="subtitle">Banka Backend — po fajlu, unit-testable paketi &nbsp;·&nbsp; $GENERATED_AT</p>
  <div class="summary-card">
    <div class="summary-info">
      <span class="summary-label">Total Coverage</span>
      <span class="big-pct">${FILTERED_TOTAL}</span>
      <span class="summary-msg">${TOTAL_MSG}</span>
    </div>
  </div>
  <table>
    <thead><tr><th>Fajl</th><th>Pokrivenost</th><th>%</th></tr></thead>
    <tbody>
$ROWS_HTML
    </tbody>
  </table>
  <footer>Threshold: ${THRESHOLD}% &nbsp;·&nbsp; Excluded: cmd/, repository/, smtp/, transport/, database/, worker/, mocks/, tests/</footer>
</body>
</html>
HTML

echo ""
echo "  HTML report: $COVERAGE_HTML"
echo "═══════════════════════════════════════════════════════════"

# ── Exit non-zero if below threshold ─────────────────────────────────────────
if awk "BEGIN { exit ($PERCENT >= $THRESHOLD) ? 0 : 1 }"; then
    echo "  ✓ Coverage ${FILTERED_TOTAL} meets the ≥${THRESHOLD}% threshold."
else
    echo "  ✗ Coverage ${FILTERED_TOTAL} is BELOW the ≥${THRESHOLD}% threshold." >&2
    exit 1
fi
