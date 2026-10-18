#!/usr/bin/env bash
set -euo pipefail

usage() {
  printf 'Usage: %s [NEW_OUTPUT_DIRECTORY]\n' "${0##*/}" >&2
  printf 'Creates an Ed25519 assertion keypair; it never prints the private key.\n' >&2
}

if [[ $# -gt 1 ]]; then
  usage
  exit 2
fi

if ! command -v openssl >/dev/null 2>&1; then
  printf 'error: OpenSSL is required to create the dashboard assertion keypair.\n' >&2
  exit 1
fi

if [[ -n ${1-} ]]; then
  output_dir=$1
else
  if [[ -z ${HOME-} ]]; then
    printf 'error: set HOME or pass an output directory.\n' >&2
    exit 1
  fi
  output_dir="${XDG_CONFIG_HOME:-$HOME/.config}/scopra/dashboard-keys"
fi

if [[ -e $output_dir || -L $output_dir ]]; then
  printf 'error: output path already exists; refusing to overwrite: %s\n' "$output_dir" >&2
  exit 1
fi

parent_dir=$(dirname -- "$output_dir")
mkdir -p -- "$parent_dir"
mkdir -m 700 -- "$output_dir"
created_dir=1
work_dir=''
cleanup() {
  if [[ -n $work_dir && -d $work_dir ]]; then
    rm -rf -- "$work_dir"
  fi
  if [[ ${created_dir:-0} -eq 1 && -d $output_dir ]]; then
    rm -f -- "$output_dir/dashboard-jwt-private.pem" "$output_dir/dashboard-jwt-public.pem"
    rmdir -- "$output_dir" 2>/dev/null || true
  fi
}
trap cleanup EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM
umask 077
work_dir=$(mktemp -d "$output_dir/.keygen.XXXXXX")
openssl genpkey -algorithm ED25519 -out "$work_dir/dashboard-jwt-private.pem" >/dev/null 2>&1
openssl pkey -in "$work_dir/dashboard-jwt-private.pem" -pubout -out "$work_dir/dashboard-jwt-public.pem" >/dev/null 2>&1
chmod 600 "$work_dir/dashboard-jwt-private.pem" "$work_dir/dashboard-jwt-public.pem"
mv -- "$work_dir/dashboard-jwt-private.pem" "$output_dir/dashboard-jwt-private.pem"
mv -- "$work_dir/dashboard-jwt-public.pem" "$output_dir/dashboard-jwt-public.pem"
rmdir -- "$work_dir"
work_dir=''
created_dir=0
trap - EXIT HUP INT TERM

printf 'Created Ed25519 dashboard assertion keys (directory mode 0700; files mode 0600):\n'
printf '  private: %s/dashboard-jwt-private.pem  [configure only in the dashboard server environment]\n' "$output_dir"
printf '  public:  %s/dashboard-jwt-public.pem  [configure only in Caloris]\n' "$output_dir"
public_env=$(awk '{ if (NR > 1) printf "\\n"; printf "%s", $0 }' "$output_dir/dashboard-jwt-public.pem")
printf '\nCaloris public-key environment entry (safe to copy into .env.production):\n'
printf 'SCOPRA_DASHBOARD_JWT_PUBLIC_KEY=%s\n' "$public_env"
printf '\nThe private key was not printed. Do not commit either file or paste them into chat.\n'
