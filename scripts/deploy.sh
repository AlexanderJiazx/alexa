#!/bin/sh
# deploy.sh brings this checkout to a commit of main, rebuilds the image and
# restarts Alexa, and puts the previous image back if the new one doesn't
# come up healthy. The deploy workflow (.github/workflows/deploy.yml) runs it
# over SSH as the forced command of a key that can do nothing else, so the
# commit arrives in SSH_ORIGINAL_COMMAND. By hand: scripts/deploy.sh [commit]
set -eu
cd "$(dirname "$0")/.."

want=${1:-${SSH_ORIGINAL_COMMAND:-}}
case "$want" in
'') want=origin/main ;;
*[!0-9a-f]*)
	echo "deploy: not a commit id: $want" >&2
	exit 2
	;;
esac

# One deploy at a time; a second waits for the first.
exec 9>"${HOME:-/tmp}/.alexa-deploy.lock"
flock -w 1800 9

git fetch --quiet origin main
sha=$(git rev-parse --verify --quiet "$want^{commit}") || {
	echo "deploy: no such commit: $want" >&2
	exit 2
}
# Only what's on main goes out.
if ! git merge-base --is-ancestor "$sha" origin/main; then
	echo "deploy: $sha isn't on main" >&2
	exit 2
fi
git checkout --quiet --force --detach "$sha"
echo "deploy: building $(git log -1 --format='%h %s')"

prev=$(docker image inspect -f '{{.Id}}' alexa:latest 2>/dev/null || true)
docker compose build --pull --quiet
docker compose up -d --remove-orphans

# The image's health check passes once the server answers /api/health.
healthy() {
	for _ in $(seq 1 60); do
		case "$(docker inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{end}}' alexa 2>/dev/null)" in
		healthy) return 0 ;;
		unhealthy) return 1 ;;
		esac
		sleep 3
	done
	return 1
}

if healthy; then
	now=$(docker image inspect -f '{{.Id}}' alexa:latest)
	if [ -n "$prev" ] && [ "$prev" != "$now" ]; then
		docker image rm "$prev" >/dev/null 2>&1 || true
	fi
	echo "deploy: $(git log -1 --format=%h) is up"
	exit 0
fi

echo "deploy: the new build isn't healthy; its last logs:" >&2
docker compose logs --no-color --tail 60 alexa >&2 || true
if [ -n "$prev" ]; then
	echo "deploy: putting the previous image back" >&2
	docker tag "$prev" alexa:latest
	docker compose up -d --force-recreate
	healthy && echo "deploy: the previous build is running again" >&2
fi
exit 1
