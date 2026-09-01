# crier, my personal new-grad job alert bot
#
# `make deploy` needs DEPLOY_HOST set, either in the env or inline:
#   make deploy DEPLOY_HOST=crier@203.0.113.7

BINARY  := crier
MODULE  := github.com/Lavanic/crier
# GOARCH=arm64 for a raspberry pi target
GOARCH  ?= amd64

.PHONY: build run dry-run test smoke deploy links clean

# how many rows `make links` shows
N ?= 15

build:
	go build -o $(BINARY) ./cmd/crier

run: build
	./$(BINARY)

dry-run: build
	./$(BINARY) --dry-run

test:
	go test ./...

# smoke tests hit the real ATS endpoints, behind a build tag so
# plain `make test` stays fast and offline
smoke:
	go test -tags smoke -v -run Smoke ./...

# cross-compile for the server and ship binary + configs + systemd units.
# CGO_ENABLED=0 is fine because modernc sqlite is pure go (no C compiler needed).
# expects root ssh. idempotent, rerun after any change.
# notes: timer stops during deploy so a mid-tick binary swap can't hit
# ETXTBSY, binary lands via .new + mv (atomic), creds get chmod 600
# so they don't sit world-readable on the server
deploy:
	@test -n "$(DEPLOY_HOST)" || (echo "set DEPLOY_HOST=user@host" && exit 1)
	CGO_ENABLED=0 GOOS=linux GOARCH=$(GOARCH) go build -o dist/$(BINARY) ./cmd/crier
	ssh $(DEPLOY_HOST) 'id -u crier >/dev/null 2>&1 || useradd -r -s /usr/sbin/nologin crier; mkdir -p /opt/crier; systemctl stop crier.timer 2>/dev/null || true'
	scp dist/$(BINARY) $(DEPLOY_HOST):/usr/local/bin/$(BINARY).new
	scp config.yaml config.local.yaml $(DEPLOY_HOST):/opt/crier/
	scp systemd/crier.service systemd/crier.timer $(DEPLOY_HOST):/etc/systemd/system/
	ssh $(DEPLOY_HOST) 'mv /usr/local/bin/$(BINARY).new /usr/local/bin/$(BINARY) && chown -R crier:crier /opt/crier && chmod 600 /opt/crier/config.local.yaml && systemctl daemon-reload && systemctl enable --now crier.timer'

# pulls the last N alerted jobs off the server. the title is an OSC 8
# terminal hyperlink (ctrl+click to open), so the raw url never gets
# printed. workday urls run 150+ chars and used to wrap across rows,
# which broke both clicking and copy/paste. needs local sqlite3.
# override the count with N=30, or RAW=1 to print bare urls instead
links:
	@test -n "$(DEPLOY_HOST)" || (echo "set DEPLOY_HOST=user@host" && exit 1)
	@tmp=$$(mktemp) && scp -q $(DEPLOY_HOST):/opt/crier/crier.db $$tmp && \
	if [ -n "$(RAW)" ]; then \
		sqlite3 $$tmp "select url from jobs where notified_at is not null order by notified_at desc limit $(N)"; \
	else \
		w=$$(tput cols 2>/dev/null || echo 100); t=$$((w - 42)); \
		if [ $$t -lt 30 ]; then t=30; fi; \
		sqlite3 $$tmp "select printf('%-16s  %-20s ', substr(datetime(notified_at,'unixepoch','localtime'),1,16), substr(company,1,20)) || char(27) || ']8;;' || url || char(7) || substr(title,1,$$t) || char(27) || ']8;;' || char(7) from jobs where notified_at is not null order by notified_at desc limit $(N)"; \
	fi; \
	rm -f $$tmp

clean:
	rm -rf $(BINARY) dist/
