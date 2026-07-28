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
# Watch the disk on a long run: a full pass at 300s took the go-build cache to
# 17G here and filled the volume, which surfaces as a bogus "failure" (the test
# harness cannot make a TempDir). `go clean -cache -fuzzcache` afterwards.
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
  # Go saves the offending input on ANY failure during fuzzing, including ones
  # that are nothing to do with the code: a full disk, an fd limit, a killed
  # worker. So "an input was saved" does not mean "a defect was found" -- checking
  # only for the file reported a disk-full failure as a parser bug on this
  # script's first real use, and left a healthy input sitting in testdata/fuzz
  # masquerading as a regression seed. Screen the environmental failures first.
  if printf '%s' "$output" | grep -qE 'no space left on device|too many open files|cannot allocate memory|signal: killed'; then
    inconclusive=1
    echo "inconclusive (environment, not the code -- rerun after fixing it)"
    printf '%s\n' "$output" | grep -oE '[^ ]*(no space left on device|too many open files|cannot allocate memory|signal: killed)' | head -1 | sed 's/^/    /'
    # Nothing was learned about the code, so do not leave a "crasher" behind.
    rm -rf "${pkg%/}/testdata/fuzz/${target}"
    continue
  fi
  # A real find writes the input under testdata/fuzz/<Target>/. Without one, the
  # failure is the fuzzing harness itself -- most often "context deadline
  # exceeded", which a loaded machine produces at short -fuzztime and which does
  # not reproduce.
  crasher_dir="${pkg%/}/testdata/fuzz/${target}"
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
