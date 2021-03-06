#!/bin/bash

# We can't test that control-tower will update itself to a latest release without publishing a new release
# Instead we will test that if we publish a non-existant release, the self-update will revert back to a known release

# shellcheck disable=SC1091
source control-tower/ci/tasks/lib/test-setup.sh
# shellcheck disable=SC1091
source control-tower/ci/tasks/lib/check-cidr-ranges.sh

handleVerboseMode

setDeploymentName upt

trapDefaultCleanup

cp release/control-tower-linux-amd64 ./cup
chmod +x ./cup

echo "DEPLOY OLD VERSION"

./cup deploy "$deployment"
assertNetworkCidrsCorrect

# Assigning a subshell to a variable fails fast; eval "$(... doesn't
info_output="$(./cup info --env "$deployment")"
eval "$info_output"
config=$(./cup info --json "$deployment")
[[ -n $config ]]
domain=$(echo "$config" | jq -r '.config.domain')

echo "Waiting for bosh lock to become available"
wait_time=0
until [[ $(bosh locks --json | jq -r '.Tables[].Rows | length') -eq 0 ]]; do
  (( ++wait_time ))
  if [[ $wait_time -ge 10 ]]; then
    echo "Waited too long for lock" && exit 1
  fi
  printf '.'
  sleep 60
done
echo "Bosh lock available - Proceeding"

echo "UPDATE TO NEW VERSION"
rm -rf cup
cp "$BINARY_PATH" ./cup
chmod +x ./cup
./cup deploy "$deployment"

echo "Waiting for 30 seconds to let detached upgrade start"
sleep 30

echo "Waiting for update to complete"
wait_time=0
until curl -skIfo/dev/null "https://$domain"; do
  (( ++wait_time ))
  if [[ $wait_time -ge 10 ]]; then
    echo "Waited too long for deployment" && exit 1
  fi
  printf '.'
  sleep 30
done
echo "Update complete - Proceeding"

sleep 60

config=$(./cup info --json "$deployment")
domain=$(echo "$config" | jq -r '.config.domain')
# shellcheck disable=SC2034
username=$(echo "$config" | jq -r '.config.concourse_username')
# shellcheck disable=SC2034
password=$(echo "$config" | jq -r '.config.concourse_password')
echo "$config" | jq -r '.config.concourse_ca_cert' > generated-ca-cert.pem

# shellcheck disable=SC2034
cert="generated-ca-cert.pem"
# shellcheck disable=SC2034
manifest="$(dirname "$0")/hello.yml"
# shellcheck disable=SC2034
job="hello"

assertPipelineIsSettableAndRunnable
assertNetworkCidrsCorrect
