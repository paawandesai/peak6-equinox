#!/usr/bin/env bash
# One-time GCP setup for Equinox (M1-02, D-002, D-016, D-034).
#
# Idempotent: every resource is checked before it is created, so the script
# can be re-run safely. Each step is echoed before it runs. Generated
# passwords are written straight to Secret Manager and never printed (AB-06).
#
# Needs: gcloud authenticated with Owner (or equivalent) on the project, openssl.
# Env overrides: GCP_PROJECT, GCP_REGION.
set -euo pipefail

PROJECT="${GCP_PROJECT:-peak6-equinox-510816}"
REGION="${GCP_REGION:-us-central1}"

AR_REPO="equinox"
SQL_INSTANCE="equinox-pg"
SQL_TIER="db-f1-micro" # smallest shared-core tier
DB_USER="equinox"
DATABASES=(equinox equinox_staging)
RUN_SA_NAME="equinox-run"
RUN_SA="${RUN_SA_NAME}@${PROJECT}.iam.gserviceaccount.com"
RUN_SA_ROLES=(roles/cloudsql.client roles/secretmanager.secretAccessor)
SECRET_DB_URL="equinox-database-url"
SECRET_STAGING_DB_URL="equinox-staging-database-url"
SECRET_ADMIN_PASSWORD="equinox-admin-password"
SECRET_ANTHROPIC_KEY="equinox-anthropic-key"
APIS=(
  run.googleapis.com
  sqladmin.googleapis.com
  secretmanager.googleapis.com
  cloudscheduler.googleapis.com
  artifactregistry.googleapis.com
  cloudbuild.googleapis.com
)

# step prints what is about to happen.
step() { printf '\n==> %s\n' "$*"; }
# skip reports a resource that already exists.
skip() { printf '    exists, skipping: %s\n' "$*"; }
# gc runs gcloud against the project, without interactive prompts.
gc() { gcloud --project="$PROJECT" --quiet "$@"; }

# run echoes a gc command to stderr, then runs it. Never pass secret values through run.
run() {
  {
    printf '    + gcloud --project=%q --quiet' "$PROJECT"
    printf ' %q' "${@:2}"
    printf '\n'
  } >&2
  "$@"
}

secret_exists() { gc secrets describe "$1" >/dev/null 2>&1; }

# put_secret stores the value read from stdin as a new version of secret $1,
# creating the secret first if needed. The value is never echoed.
put_secret() {
  if secret_exists "$1"; then
    gc secrets versions add "$1" --data-file=- >/dev/null
  else
    gc secrets create "$1" --replication-policy=automatic --data-file=- >/dev/null
  fi
}

# random_hex prints 2*$1 random hex characters.
random_hex() { openssl rand -hex "$1"; }

# db_url builds a DSN that reaches Cloud SQL over the unix socket Cloud Run
# mounts for --add-cloudsql-instances.
db_url() {
  local password="$1" db="$2"
  printf 'postgres://%s:%s@/%s?host=/cloudsql/%s:%s:%s' \
    "$DB_USER" "$password" "$db" "$PROJECT" "$REGION" "$SQL_INSTANCE"
}

enable_apis() {
  step "Enable APIs: ${APIS[*]}"
  local enabled missing=()
  enabled="$(gc services list --enabled --format='value(config.name)')"
  for api in "${APIS[@]}"; do
    if grep -qx "$api" <<<"$enabled"; then skip "$api"; else missing+=("$api"); fi
  done
  if ((${#missing[@]})); then
    run gc services enable "${missing[@]}"
  fi
}

create_ar_repo() {
  step "Artifact Registry repo '$AR_REPO' in $REGION"
  if gc artifacts repositories describe "$AR_REPO" --location="$REGION" >/dev/null 2>&1; then
    skip "$AR_REPO"
  else
    run gc artifacts repositories create "$AR_REPO" --location="$REGION" \
      --repository-format=docker --description="Equinox container images"
  fi
}

create_sql_instance() {
  step "Cloud SQL Postgres 16 instance '$SQL_INSTANCE' ($SQL_TIER, $REGION); creation takes several minutes"
  if gc sql instances describe "$SQL_INSTANCE" >/dev/null 2>&1; then
    skip "$SQL_INSTANCE"
  else
    run gc sql instances create "$SQL_INSTANCE" --database-version=POSTGRES_16 \
      --edition=enterprise --tier="$SQL_TIER" --region="$REGION" \
      --storage-size=10 --storage-auto-increase
  fi
}

create_databases() {
  for db in "${DATABASES[@]}"; do
    step "Database '$db' on $SQL_INSTANCE"
    if gc sql databases describe "$db" --instance="$SQL_INSTANCE" >/dev/null 2>&1; then
      skip "$db"
    else
      run gc sql databases create "$db" --instance="$SQL_INSTANCE"
    fi
  done
}

# create_db_user_and_urls sets a fresh generated password for the DB user and
# stores the prod and staging DSNs. It runs only when a DSN secret is missing,
# so a re-run never rotates a working password.
create_db_user_and_urls() {
  step "Database user '$DB_USER' and secrets '$SECRET_DB_URL', '$SECRET_STAGING_DB_URL' (password generated, not printed)"
  if secret_exists "$SECRET_DB_URL" && secret_exists "$SECRET_STAGING_DB_URL"; then
    skip "$SECRET_DB_URL, $SECRET_STAGING_DB_URL"
    return
  fi
  local password
  password="$(random_hex 24)"
  if gc sql users list --instance="$SQL_INSTANCE" --format='value(name)' | grep -qx "$DB_USER"; then
    echo "    setting a new password for existing user $DB_USER"
    gc sql users set-password "$DB_USER" --instance="$SQL_INSTANCE" --password="$password" >/dev/null
  else
    echo "    creating user $DB_USER"
    gc sql users create "$DB_USER" --instance="$SQL_INSTANCE" --password="$password" >/dev/null
  fi
  echo "    storing $SECRET_DB_URL"
  db_url "$password" equinox | put_secret "$SECRET_DB_URL"
  echo "    storing $SECRET_STAGING_DB_URL"
  db_url "$password" equinox_staging | put_secret "$SECRET_STAGING_DB_URL"
}

create_service_account() {
  step "Service account '$RUN_SA'"
  if gc iam service-accounts describe "$RUN_SA" >/dev/null 2>&1; then
    skip "$RUN_SA"
  else
    run gc iam service-accounts create "$RUN_SA_NAME" --display-name="Equinox Cloud Run"
  fi
  local bound
  bound="$(gc projects get-iam-policy "$PROJECT" --flatten='bindings[].members' \
    --filter="bindings.members:serviceAccount:$RUN_SA" --format='value(bindings.role)')"
  for role in "${RUN_SA_ROLES[@]}"; do
    step "Grant $role to $RUN_SA"
    if grep -qx "$role" <<<"$bound"; then
      skip "$role"
    else
      run gc projects add-iam-policy-binding "$PROJECT" \
        --member="serviceAccount:$RUN_SA" --role="$role" --condition=None >/dev/null
    fi
  done
}

create_app_secrets() {
  step "Secret '$SECRET_ADMIN_PASSWORD' (random, 32 characters, not printed)"
  if secret_exists "$SECRET_ADMIN_PASSWORD"; then
    skip "$SECRET_ADMIN_PASSWORD"
  else
    random_hex 16 | put_secret "$SECRET_ADMIN_PASSWORD"
  fi

  # Secret Manager keeps no version for an empty payload, so the placeholder is
  # a secret with no versions. make deploy mounts it once a real key is added.
  step "Secret '$SECRET_ANTHROPIC_KEY' (empty placeholder)"
  if secret_exists "$SECRET_ANTHROPIC_KEY"; then
    skip "$SECRET_ANTHROPIC_KEY"
  else
    printf '' | put_secret "$SECRET_ANTHROPIC_KEY"
  fi
}

print_manual_steps() {
  cat <<EOF

==> Done. Manual steps left for the human (see docs/status.md Q-3):
  1. Set a \$50 budget alert: Cloud console > Billing > Budgets & alerts > Create budget,
     scoped to project $PROJECT, amount \$50, alerts at 50%, 90% and 100%.
  2. Set a monthly spend limit in the Anthropic console (Settings > Limits).
  3. Paste the real Anthropic key (it is read from stdin, not from the command line):
       gcloud secrets versions add $SECRET_ANTHROPIC_KEY --project=$PROJECT --data-file=-
     then type or paste the key and press Ctrl-D.
  4. Deploy: make deploy (prod) or make deploy-staging.
EOF
}

main() {
  command -v gcloud >/dev/null || { echo "gcloud not found" >&2; exit 1; }
  command -v openssl >/dev/null || { echo "openssl not found" >&2; exit 1; }
  echo "Bootstrapping project $PROJECT in $REGION"
  enable_apis
  create_ar_repo
  create_sql_instance
  create_databases
  create_db_user_and_urls
  create_service_account
  create_app_secrets
  print_manual_steps
}

main "$@"
