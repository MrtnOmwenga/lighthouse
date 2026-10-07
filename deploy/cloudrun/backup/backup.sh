#!/bin/sh
# Nightly backups, and the weekly test that they can be restored.
#
#   backup        dump both PostgreSQL databases and GhostChat's MongoDB, and store them
#   restore-test  fetch the latest dumps, restore them into a scratch PostgreSQL started inside
#                 this container, check what came back, and record how long it took
#
# Runs as a Cloud Run job in the stock PostgreSQL image (which has pg_dump, pg_restore and a
# server to restore into). Objects go to a Cloud Storage bucket: one copy named by date, kept 30
# days, and one under latest/, which is what the restore test reads.
#
# Every failure prints a line starting "BACKUP FAILED" or "RESTORE TEST FAILED": an alert
# matches on those words.
set -eu

MODE="${1:-backup}"
DAY="$(date -u +%F)"
WORK="$(mktemp -d)"
WORDS="BACKUP FAILED"
[ "$MODE" = restore-test ] && WORDS="RESTORE TEST FAILED"

fail() { echo "$WORDS: $*"; exit 1; }
# Anything that stops the script early, without a message of its own, still says so.
trap 'code=$?; [ "$code" -eq 0 ] || [ -n "${SAID:-}" ] || echo "$WORDS: stopped early with status $code"' EXIT
said() { SAID=1; fail "$@"; }

# curl, not the image's built-in wget: BusyBox wget cuts an uploaded file at its first zero byte,
# which turned every dump into seven bytes. (The restore test is what noticed.)
command -v curl >/dev/null || [ -n "${LOCAL_STORE:-}" ] || apk add --no-cache --quiet curl || said "could not install curl"

token() {
  curl -sS --fail -H 'Metadata-Flavor: Google' \
    http://metadata.google.internal/computeMetadata/v1/instance/service-accounts/default/token |
    sed -E 's/.*"access_token" *: *"([^"]+)".*/\1/'
}
encode() { printf %s "$1" | sed 's#/#%2F#g'; }
# LOCAL_STORE (a directory) stands in for the bucket when the script is tried outside Google.
put() { # file, object name. Fails unless the bucket reports the same number of bytes.
  if [ -n "${LOCAL_STORE:-}" ]; then mkdir -p "$(dirname "$LOCAL_STORE/$2")" && cp "$1" "$LOCAL_STORE/$2"; return; fi
  answer="$(curl -sS --fail -X POST -H "Authorization: Bearer $(token)" -H 'Content-Type: application/octet-stream' \
    --data-binary "@$1" "https://storage.googleapis.com/upload/storage/v1/b/$BUCKET/o?uploadType=media&name=$(encode "$2")")" || return 1
  stored="$(printf %s "$answer" | tr -d '\n' | sed -E 's/.*"size" *: *"([0-9]+)".*/\1/')"
  [ "$stored" = "$(wc -c <"$1" | tr -d ' ')" ] || { echo "stored $stored bytes of $(wc -c <"$1") for $2"; return 1; }
}
get() { # object name, file
  if [ -n "${LOCAL_STORE:-}" ]; then cp "$LOCAL_STORE/$1" "$2"; return; fi
  curl -sS --fail -o "$2" -H "Authorization: Bearer $(token)" "https://storage.googleapis.com/storage/v1/b/$BUCKET/o/$(encode "$1")?alt=media"
}
store() { # file, name: by date and as the latest
  put "$1" "$DAY/$2" || said "could not store $DAY/$2"
  put "$1" "latest/$2" || said "could not store latest/$2"
  echo "stored $2 ($(wc -c <"$1") bytes)"
}

# The databases: name, connection address, and the variable holding its owner's password.
DATABASES="lighthouse|${LIGHTHOUSE_URL:-}|${LIGHTHOUSE_PASSWORD:-} redacted|${REDACTED_URL:-}|${REDACTED_PASSWORD:-}"

backup() {
  for entry in $DATABASES; do
    name="${entry%%|*}"; rest="${entry#*|}"; url="${rest%%|*}"; password="${rest#*|}"
    [ -n "$url" ] || { echo "skipping $name: no address given"; continue; }
    PGPASSWORD="$password" pg_dump --format=custom --file "$WORK/$name.dump" "$url" || said "pg_dump of $name"
    [ "$(wc -c <"$WORK/$name.dump")" -gt 1000 ] || said "the dump of $name is implausibly small"
    store "$WORK/$name.dump" "$name.dump"
  done
  if [ -n "${MONGODB_URI:-}" ]; then
    command -v mongodump >/dev/null || apk add --no-cache --quiet mongodb-tools || said "could not install mongodump"
    mongodump --quiet --uri "$MONGODB_URI" --gzip --archive="$WORK/ghostchat.archive.gz" || said "mongodump of ghostchat"
    store "$WORK/ghostchat.archive.gz" ghostchat.archive.gz
  else
    echo "skipping ghostchat: no address given"
  fi
  printf '%s\n' "$DAY" >"$WORK/date" && put "$WORK/date" latest/date || said "could not record the date"
  echo "BACKUP OK $DAY"
}

sql() { gosu postgres psql -h "$WORK" -U postgres -d "$1" -AtX -v ON_ERROR_STOP=1 -c "$2"; }

restore_test() {
  started="$(date +%s)"
  get latest/date "$WORK/date" || said "there is no backup to test"
  taken="$(cat "$WORK/date")"
  age_days=$(( ( $(date -u +%s) - $(date -u -d "$taken" +%s) ) / 86400 ))
  [ "$age_days" -le 1 ] || said "the latest backup is $age_days days old ($taken)"

  # A scratch server, reachable only through a socket inside this container.
  chown postgres "$WORK"
  gosu postgres initdb -D "$WORK/pg" -U postgres >/dev/null 2>"$WORK/initdb.log" || said "could not start a scratch database"
  gosu postgres pg_ctl -D "$WORK/pg" -w -l "$WORK/pg.log" -o "-c listen_addresses='' -k $WORK" start >/dev/null || said "could not start a scratch database"

  report=""
  for entry in $DATABASES; do
    name="${entry%%|*}"
    get "latest/$name.dump" "$WORK/$name.dump" 2>/dev/null || { [ -n "${REQUIRE:-}" ] && said "no dump of $name"; echo "no dump of $name: skipped"; continue; }
    chmod 644 "$WORK/$name.dump"
    gosu postgres createdb -h "$WORK" -U postgres "$name"
    # Owners and grants name roles that only exist on the real server; the data and structure are
    # what is being tested here.
    gosu postgres pg_restore -h "$WORK" -U postgres -d "$name" --no-owner --no-privileges "$WORK/$name.dump" 2>"$WORK/$name.err" || true
    tables="$(sql "$name" "SELECT count(*) FROM pg_tables WHERE schemaname = 'public'")"
    [ "$tables" -gt 0 ] || { cat "$WORK/$name.err"; said "$name restored with no tables"; }
    rows="$(sql "$name" "SELECT coalesce(sum(n_live_tup), 0) FROM pg_stat_user_tables" 2>/dev/null || echo 0)"
    extra=""
    if [ "$name" = lighthouse ]; then
      monitors="$(sql "$name" "SELECT count(*) FROM monitors")"
      newest="$(sql "$name" "SELECT coalesce(floor(extract(epoch FROM now() - max(at)) / 3600)::int, -1) FROM checks")"
      [ "$monitors" -gt 0 ] || said "lighthouse restored with no monitors"
      { [ "$newest" -ge 0 ] && [ "$newest" -le 26 ]; } || said "lighthouse's newest check in the backup is $newest hours old"
      extra=" monitors=$monitors newest_check_hours=$newest"
    fi
    report="$report $name:tables=$tables$extra"
    echo "restored $name: $tables tables$extra ($(grep -c . "$WORK/$name.err" || true) notes from pg_restore)"
  done

  if get latest/ghostchat.archive.gz "$WORK/ghostchat.archive.gz" 2>/dev/null; then
    # There is no MongoDB server here to restore into (mongorestore needs one even for a dry
    # run), so this is a weaker check than PostgreSQL gets: the archive decompresses without
    # error from start to finish, and isn't empty.
    gzip -t "$WORK/ghostchat.archive.gz" || said "the ghostchat archive is damaged"
    bytes="$(gzip -dc "$WORK/ghostchat.archive.gz" | wc -c | tr -d ' ')"
    [ "$bytes" -gt 0 ] || said "the ghostchat archive is empty"
    report="$report ghostchat:archive_bytes=$bytes"
    echo "ghostchat: archive intact, $bytes bytes uncompressed"
  fi
  [ -n "$report" ] || said "nothing was restored"

  gosu postgres pg_ctl -D "$WORK/pg" -m fast stop >/dev/null || true
  seconds=$(( $(date +%s) - started ))
  line="RESTORE TEST OK backup=$taken seconds=$seconds$report"
  echo "$line" >"$WORK/result" && put "$WORK/result" "restore-tests/$DAY.txt" || said "could not record the result"
  echo "$line"
}

case "$MODE" in
  backup) backup ;;
  restore-test) restore_test ;;
  *) said "unknown mode $MODE" ;;
esac
