#!/usr/bin/env bash
# M1.5 smoke fragment: request + decide a Review (and prove the decision is one-shot);
# seal a Session, verify the archive blob structure, the RecoveryCheckpoint head commit,
# and that both survive a kernel restart.
set -euo pipefail
V=".bin/vibe -socket $SOCK -identity local-cli -token $TOKEN"
RAW=".bin/vibe-raw -socket $SOCK -identity local-cli -token $TOKEN"

SRC="$DATA/rssrc"; mkdir -p "$SRC"
git -C "$SRC" -c init.defaultBranch=main init -q
printf 'class Calc {}\n' > "$SRC/Calc.java"
git -C "$SRC" add -A
git -C "$SRC" -c user.email=s@t -c user.name=s -c commit.gpgsign=false commit -q -m init

WC_ID="$($V task create -title "rs smoke" -goal g -repo "$SRC" | sed -n 's/.*wc \([^ ]*\).*/\1/p')"
WT="$($V workspace allocate "$WC_ID" -repo "$SRC" | sed -n 's/.*path \([^ ]*\).*/\1/p')"
RUN_ID="$($V agent run "$WC_ID" -workspace "$WT" -prompt p -steps 2 -write-file Calc.java -write-content '// touched
' | sed -n 's/.*agent_run \([^ ]*\).*/\1/p')"
sleep 0.5

# --- review: request, decide once, and prove a second decision is rejected ---
rev_out="$($V review request "$WC_ID" -agent-run "$RUN_ID" -diff-artifact art-placeholder -evidence build:PASS -evidence test:PASS)"
REV_ID="$(echo "$rev_out" | sed -n 's/^review \([^ ]*\).*/\1/p')"
echo "$rev_out" | grep -q 'status PENDING' || { echo "FAIL: review not PENDING: $rev_out"; exit 1; }
$V review decide "$REV_ID" -approved -reviewer alice -acceptance AC1=pass | grep -q 'status APPROVED' \
  || { echo "FAIL: review decide"; exit 1; }
$V review show "$REV_ID" | grep -q APPROVED || { echo "FAIL: review show"; exit 1; }

# The second decide MUST fail. Under `set -o pipefail` a non-zero exit anywhere in a
# pipeline fails the script even when the downstream grep matches, so capture the
# expected failure first and only then assert on its text.
second_rc=0
second_out="$($V review decide "$REV_ID" -changes-requested 2>&1)" || second_rc=$?
[ "$second_rc" -ne 0 ] || { echo "FAIL: second decide should have exited non-zero: $second_out"; exit 1; }
echo "$second_out" | grep -qi 'conflict\|error' \
  || { echo "FAIL: second decide should be rejected as a conflict: $second_out"; exit 1; }

# --- seal the session ---
# Note: the append handler stamps correlation_id from the envelope and the CLI mints a
# fresh one per call, so filter-by-correlation cannot select events appended from here.
# Per the plan's canonical-event degradation clause the smoke seals without event_ids
# and asserts the archive, a non-empty hash, and the checkpoint head commit instead.
seal_out="$($V session seal "$WC_ID" -agent-run "$RUN_ID" -workspace "$WT" -correlation "$WC_ID")"
SESS_ID="$(echo "$seal_out" | sed -n 's/^session \([^ ]*\).*/\1/p')"
ARCH="$(echo "$seal_out" | sed -n 's/.*archive \([^ ]*\) .*/\1/p')"
echo "$seal_out" | grep -qE 'hash [0-9a-f]{12}' || { echo "FAIL: no archive hash: $seal_out"; exit 1; }
echo "$seal_out" | grep -qE 'head [0-9a-f]{12}' || { echo "FAIL: no head commit in checkpoint: $seal_out"; exit 1; }
$RAW -cap blob.get -kind query -service default-blob -authority blob-main -payload "{\"uri\":\"$ARCH\"}" \
  | python3 -c 'import sys,json,base64; d=json.load(sys.stdin); a=json.loads(base64.b64decode(d["content_base64"])); assert "recovery_checkpoint" in a and "canonical_events" in a and "session_record" in a, sorted(a)' \
  || { echo "FAIL: archive blob missing structure"; exit 1; }

# --- restart survival ---
restart_kernel
for _ in $(seq 1 50); do $V session show "$SESS_ID" 2>/dev/null | grep -q "$SESS_ID" && break; sleep 0.1; done
$V session show "$SESS_ID" 2>/dev/null | grep -q "$SESS_ID" || { echo "FAIL: session lost on restart"; exit 1; }
$V review show "$REV_ID" 2>/dev/null | grep -q APPROVED || { echo "FAIL: review lost on restart"; exit 1; }

echo "M1.5 REVIEW+SESSION SMOKE: OK"
