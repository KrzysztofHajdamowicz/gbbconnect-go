#!/usr/bin/with-contenv bashio

set -euo pipefail

readonly config_path="/data/options.json"
readonly rendered_config="/tmp/gbbconnect.json"
readonly state_dir="/data/state"

if [[ ! -f "${config_path}" ]]; then
  bashio::log.fatal "Home Assistant options file is missing: ${config_path}"
  exit 1
fi

umask 077
if ! jq -e -f /usr/share/gbbconnect/render.jq \
  "${config_path}" >"${rendered_config}.tmp"; then
  bashio::log.fatal "Home Assistant options are not a valid gbbconnect configuration"
  exit 1
fi
mv "${rendered_config}.tmp" "${rendered_config}"

if ! /usr/local/bin/gbbconnect config validate \
  --config "${rendered_config}"; then
  bashio::log.fatal "Rendered Home Assistant options failed validation"
  exit 1
fi

bashio::log.info "Starting gbbconnect-go"
exec /usr/local/bin/gbbconnect run \
  --config "${rendered_config}" \
  --state-dir "${state_dir}"
