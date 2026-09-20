#!/usr/bin/env bash
# ═══════════════════════════════════════════════════════════════
# NK3C SIP 真机演练脚本（gophone → Go 话务域 :5060）
# 场景：真实 SIP 呼入 → IVR 提示音 → DTMF 0 → 转人工 → 工单落库 → 录音落盘 → API 回放
# 依赖：gophone（https://github.com/emiago/gophone releases）+ curl + jq（可选）
# 用法：./sip_drill.sh [SIP地址，默认 127.0.0.1:5060] [主叫号，默认 13977771234]
# ═══════════════════════════════════════════════════════════════
set -euo pipefail

SIP_ADDR="${1:-127.0.0.1:5060}"
CALLER="${2:-13977771234}"
API="${NK3C_API:-127.0.0.1:8080}"
GOPHONE="${GOPHONE:-gophone}"
PASS=0; FAIL=0

ok()   { echo "  ✓ $1"; PASS=$((PASS+1)); }
bad()  { echo "  ✗ $1"; FAIL=$((FAIL+1)); }
check(){ [ "$1" = "$2" ] && ok "$3" || bad "$3（期望 $2，实际 $1）"; }

echo "── NK3C SIP 真机演练 → $SIP_ADDR（主叫 $CALLER）──"

# ① 真实 SIP 呼入 + 5s 后 DTMF 0（转人工）
echo "① gophone 呼入并发送 DTMF 0…"
OUT=$("$GOPHONE" -t udp dial -ua "$CALLER" -media=log -dtmf=0 -dtmf_delay=5s -f \
  "sip:10086@${SIP_ADDR%%:*}:${SIP_ADDR##*:}" 2>&1 || true)
echo "$OUT" | grep -q "Sending DTMF" && ok "DTMF 已发送" || bad "DTMF 未发送"

sleep 1.5

# ② 工单自动落库
echo "② 校验转人工工单…"
TOK=$(curl -s -X POST "http://$API/api/auth/login" -H 'Content-Type: application/json' \
  -d "{\"loginName\":\"${NK3C_USER:-admin}\",\"password\":\"${NK3C_PASS:-123456}\"}" \
  | python3 -c 'import sys,json;print(json.load(sys.stdin)["data"]["sessionId"])')
WO=$(curl -s "http://$API/api/workorder?status=PENDING" -H "Authorization: Bearer $TOK")
CNT=$(echo "$WO" | python3 -c "import sys,json;print(json.load(sys.stdin)['data']['total'])")
[ "${CNT:-0}" -ge 1 ] && ok "转人工工单 PENDING ×$CNT" || bad "未发现转人工工单"
HIT=$(echo "$WO" | python3 -c "import sys,json;print(sum(1 for r in json.load(sys.stdin)['data']['rows'] if r['caller_no']=='$CALLER'))")
[ "${HIT:-0}" -ge 1 ] && ok "主叫号关联正确（$CALLER）" || bad "工单未关联主叫 $CALLER"

# ③ 呼入话务日志 + 结局
echo "③ 校验呼入话务日志…"
LOG=$(curl -s "http://$API/api/ivr/logs" -H "Authorization: Bearer $TOK")
OUTCOME=$(echo "$LOG" | python3 -c "import sys,json;rows=[r for r in json.load(sys.stdin)['data'] if r['caller_no']=='$CALLER'];print(rows[0]['outcome'] if rows else 'NONE')")
check "${OUTCOME:-NONE}" "TRANSFER:MANUAL" "结局 TRANSFER:MANUAL"
LID=$(echo "$LOG" | python3 -c "import sys,json;rows=[r for r in json.load(sys.stdin)['data'] if r['caller_no']=='$CALLER'];print(rows[0]['id'] if rows else '')")

# ④ 录音落盘 + API 回放
echo "④ 校验录音…"
REC=$(echo "$LOG" | python3 -c "import sys,json;rows=[r for r in json.load(sys.stdin)['data'] if r['caller_no']=='$CALLER'];print(rows[0].get('record_file','') if rows else '')")
[ -n "$REC" ] && ok "录音已落库：$(basename "$REC")" || bad "无录音记录"
if [ -n "$LID" ]; then
  CODE=$(curl -s -o /tmp/nk3c-drill-rec.wav -w '%{http_code}' "http://$API/api/recording/$LID" -H "Authorization: Bearer $TOK")
  SIZE=$(stat -c%s /tmp/nk3c-drill-rec.wav 2>/dev/null || echo 0)
  [ "$CODE" = "200" ] && [ "$SIZE" -gt 44 ] && ok "录音回放 200（$SIZE bytes WAV）" || bad "录音回放异常（$CODE / $SIZE bytes）"
fi

echo "── 演练结束：PASS=$PASS FAIL=$FAIL ──"
[ "$FAIL" = "0" ]
