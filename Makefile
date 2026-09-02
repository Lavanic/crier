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
# override the count with N=30, or RAW=1 to print bare urls instead.
#
# the escapes come from shell printf, NOT sqlite: sqlite 3.47+ rewrites
# control chars in its output as caret notation (^[) so a malicious db
# can't drive your terminal, which quietly defeats char(27). url reads
# first so a stray tab in a title can only mangle the title.
#
# the query runs ON the server when it has sqlite3, so only a few kb of
# rows come back. the fallback copies the whole db, which is ~35s once
# it passes 15mb and gets worse every week. times come back as unix
# ints and get formatted here, else they'd show the droplet's timezone
links:
	@test -n "$(DEPLOY_HOST)" || (echo "set DEPLOY_HOST=user@host" && exit 1)
	@w=$$(tput cols 2>/dev/null || echo 100); t=$$((w - 42)); \
	if [ $$t -lt 30 ]; then t=30; fi; \
	if [ -n "$(RAW)" ]; then \
		q="select url from jobs where notified_at is not null order by notified_at desc limit $(N);"; \
	else \
		q="select url, notified_at, substr(company,1,20), substr(title,1,$$t) from jobs where notified_at is not null order by notified_at desc limit $(N);"; \
	fi; \
	tab=$$(printf '\t'); \
	if rows=$$(printf '%s\n' "$$q" | ssh $(DEPLOY_HOST) "sqlite3 -separator '$$tab' /opt/crier/crier.db" 2>/dev/null); then :; else \
		echo "no sqlite3 on the server, copying the db instead (slow)." >&2; \
		echo "one-time fix: ssh $(DEPLOY_HOST) apt-get install -y sqlite3" >&2; \
		tmp=$$(mktemp) && scp -q $(DEPLOY_HOST):/opt/crier/crier.db $$tmp && \
		rows=$$(printf '%s\n' "$$q" | sqlite3 -separator "$$tab" $$tmp); rm -f $$tmp; \
	fi; \
	if [ -n "$(RAW)" ]; then printf '%s\n' "$$rows"; else \
		red=""; rst=""; \
		if [ -t 1 ]; then red=$$(printf '\033[1;31m'); rst=$$(printf '\033[0m'); fi; \
		cfg=config.yaml; [ -f $$cfg ] || cfg=/dev/null; \
		printf '%s\n' "$$rows" | awk -F'\t' -v CFG=$$cfg ' \
			FILENAME==CFG{ \
				if($$0~/^priority_companies:/){b="p";next} \
				if($$0~/^display_names:/){b="d";next} \
				if($$0~/^[A-Za-z_]+:/){b="";next} \
				if(b=="p"&&$$0~/^[ \t]*-/){v=$$0;sub(/^[ \t]*-[ \t]*/,"",v);sub(/[ \t]*#.*/,"",v);sub(/[ \t]+$$/,"",v);if(v!="")P[tolower(v)]=1} \
				if(b=="d"&&$$0~/^[ \t]*[^ \t#-][^:]*:/){k=$$0;sub(/:.*/,"",k);gsub(/[ \t"]/,"",k);v=$$0;sub(/^[^:]*:[ \t]*/,"",v);sub(/[ \t]*#.*/,"",v);sub(/[ \t]+$$/,"",v);gsub(/"/,"",v);if(k!="")D[tolower(k)]=v} \
				next} \
			{c=tolower($$3);n=(c in D)?tolower(D[c]):c; \
			 print (((n in P)||(tolower($$4)~/(^|[^a-z0-9])new[ -]*grad/))?1:0)"\t"$$0}' $$cfg - \
		| while IFS="$$tab" read -r crit url unix co ti; do \
			s=""; e=""; if [ "$$crit" = 1 ]; then s="$$red"; e="$$rst"; fi; \
			printf '%s%-16s  %-20s \033]8;;%s\a%s\033]8;;\a%s\n' \
				"$$s" "$$(date -d @$$unix '+%Y-%m-%d %H:%M')" "$$co" "$$url" "$$ti" "$$e"; \
		done; \
	fi

clean:
	rm -rf $(BINARY) dist/
