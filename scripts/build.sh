# Root directory
root=$(
    cd "$(dirname "${BASH_SOURCE[0]}")"
    cd .. && pwd
)

# Load the versions and constants
source "$root"/scripts/versions.sh
source "$root"/scripts/constants.sh

"$root"/scripts/build_relayer.sh
"$root"/scripts/build_signature_aggregator.sh
