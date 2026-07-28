#!/usr/bin/env bash
# Run every fuzz target in the repo for a bounded time.
#
# The targets ship with seed corpora and therefore run as ordinary unit tests on
# every `go test ./...`, which only ever exercises the seeds. That is not fuzzing:
# it proves the seeds pass, nothing more. This script runs them with -fuzz so they
# actually explore, which is the only way they find anything.
#
# There is no CI in this repo, so nothing does this for you. Run it after touching
# a parser or the chunk codec.
#
#   scripts/fuzz.sh            # 60s per target
#   scripts/fuzz.sh 300s       # longer, for a real hunt
#
# A failing input is written to internal/<pkg>/testdata/fuzz/<Target>/ and becomes
# a permanent regression seed: commit it.
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"
FUZZTIME="${1:-60s}"

# target:package, kept explicit so a newly added target has to be listed here and
# cannot be silently left unfuzzed.
TARGETS="
FuzzDecodeFrameBinary:./internal/mdsrv/
FuzzDecodeManifest:./internal/mdsrv/
FuzzFrameChunkRoundTrip:./internal/mdsrv/
FuzzParseAtomSelection:./internal/mdsrv/
FuzzParseGROFrame:./internal/mdsrv/
FuzzDecodeFrameChunk:./internal/mdsrv/
FuzzParseCheckOutput:./internal/gromacs/
FuzzExplainSelector:./internal/mvs/
FuzzJobDecode:./internal/job/
"

# Every Fuzz function in the tree must appear above, or it never gets fuzzed.
declared=$(grep -rhoE '^func (Fuzz[A-Za-z0-9_]+)' --include='*_test.go' internal/ | awk '{print $2}' | sort -u)
listed=$(echo "$TARGETS" | grep -oE '^Fuzz[A-Za-z0-9_]+' | sort -u)
missing=$(comm -23 <(echo "$declared") <(echo "$listed"))
if [ -n "$missing" ]; then
  echo "fuzz targets missing from this script:" >&2
  echo "$missing" | sed 's/^/  /' >&2
  exit 2
fi

failed=0
inconclusive=0
for entry in $TARGETS; do
  target="${entry%%:*}"
  pkg="${entry##*:}"
  printf '%-26s ' "$target"
  output="$(go test "$pkg" -run '^$' -fuzz "^${target}$" -fuzztime "$FUZZTIME" 2>&1)"
  status=$?
  execs="$(printf '%s' "$output" | grep -oE 'execs: [0-9]+' | tail -1)"
  if [ "$status" -eq 0 ]; then
    echo "ok (${execs:-seeds only})"
    continue
  fi
  # A real find writes the input under testdata/fuzz/<Target>/. Without one, the
  # failure is the fuzzing harness itself -- most often "context deadline
  # exceeded", which a loaded machine produces at short -fuzztime and which does
  # not reproduce. Reporting that as a defect would train the reader to ignore
  # this script, so it is called out as inconclusive instead.
  crasher_dir="$(dirname "${pkg%/}")/$(basename "${pkg%/}")/testdata/fuzz/${target}"
  if [ -d "$crasher_dir" ] && [ -n "$(ls -A "$crasher_dir" 2>/dev/null)" ]; then
    failed=1
    echo "FAIL -- input saved to $crasher_dir"
    printf '%s\n' "$output" | tail -25 | sed 's/^/    /'
  else
    inconclusive=1
    echo "inconclusive (harness error, no input saved -- rerun alone)"
    printf '%s\n' "$output" | grep -E '^(--- FAIL|FAIL|\s+context)' | head -3 | sed 's/^/    /'
  fi
done

if [ "$failed" -ne 0 ]; then
  echo
  echo "a failing input was saved under internal/<pkg>/testdata/fuzz/ -- commit it as a regression seed" >&2
  exit 1
fi
if [ "${inconclusive:-0}" -ne 0 ]; then
  echo
  echo "some targets did not complete; rerun them individually before trusting a clean result" >&2
  exit 3
fi
printf '\nall fuzz targets clean at %s each\n' "$FUZZTIME"
