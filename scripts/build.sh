# Root directory
root=$(
    cd "$(dirname "${BASH_SOURCE[0]}")"
    cd .. && pwd
)

"$root"/scripts/build_relayer.sh
"$root"/scripts/build_signature_aggregator.sh
