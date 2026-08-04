#!/bin/sh
set -u

repository_root=$(CDPATH='' cd -- "$(dirname -- "$0")/../../.." && pwd -P)
utc_date=$(date -u +%Y-%m-%d)
started_at=$(date -u +%Y-%m-%dT%H:%M:%SZ)
started_epoch=$(date +%s)
run_id="$(date -u +%Y%m%dT%H%M%SZ)-$(printf '%06d' "$$")"
evidence_root=${RUNGRID_EVIDENCE_ROOT:-"$repository_root/tmp/$utc_date/rungrid-headless-e2e"}

mkdir -p "$evidence_root"
run_number=1
while ! mkdir "$evidence_root/$run_number" 2>/dev/null; do
	run_number=$((run_number + 1))
done
run_directory="$evidence_root/$run_number"
output_path="$run_directory/output.txt"
result_path="$run_directory/result.json"
source_commit=$(git -C "$repository_root" rev-parse HEAD 2>/dev/null || printf unknown)
if [ -n "$(git -C "$repository_root" status --porcelain 2>/dev/null)" ]; then
	source_tree=dirty
else
	source_tree=clean
fi

finish() {
	exit_code=$1
	finished_at=$(date -u +%Y-%m-%dT%H:%M:%SZ)
	finished_epoch=$(date +%s)
	duration_seconds=$((finished_epoch - started_epoch))
	if [ "$exit_code" -eq 0 ]; then
		result=PASS
		assertion_summary="Headless lifecycle, runtime identity, generation guard, exclusive sessions, restart, and shutdown passed."
	else
		result=FAIL
		assertion_summary="The Go end-to-end test failed; inspect output.txt."
	fi
	cat >"$result_path" <<EOF
{
  "schema_version": "rungrid/test-result/v1",
  "project": "rungrid",
  "suite": "headless lifecycle",
  "test_id": "rungrid-headless-e2e",
  "environment": "local",
  "run_id": "$run_id",
  "run_number": $run_number,
  "started_at": "$started_at",
  "finished_at": "$finished_at",
  "duration_seconds": $duration_seconds,
  "result": "$result",
  "exit_code": $exit_code,
  "source_commit": "$source_commit",
  "source_tree": "$source_tree",
  "deployed_version": "NOT_APPLICABLE",
  "target_identity": "temporary XDG state with local Process Compose",
  "assertion_summary": "$assertion_summary",
  "cleanup_status": "asserted by the test and temporary-directory cleanup",
  "artifacts": ["output.txt", "result.json"]
}
EOF
	printf '%s\n' "evidence: $run_directory"
	return "$exit_code"
}

trap 'finish 130; exit 130' HUP INT TERM
if (cd "$repository_root" && RUNGRID_E2E=1 go test -run 'Test(Headless|TabOnly)LifecycleEndToEnd' -count=1 -v ./tests/end-to-end/local) >"$output_path" 2>&1; then
	cat "$output_path"
	finish 0
	exit 0
else
	exit_code=$?
	cat "$output_path"
	finish "$exit_code"
	exit "$exit_code"
fi
