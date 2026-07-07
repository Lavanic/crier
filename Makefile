# crier, my personal new-grad job alert bot
#
# `make deploy` needs DEPLOY_HOST set, either in the env or inline:
#   make deploy DEPLOY_HOST=crier@203.0.113.7

BINARY  := crier
MODULE  := github.com/Lavanic/crier

.PHONY: build run dry-run test smoke deploy clean

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

# cross-compile for the vps and ship binary + configs + systemd units.
# CGO_ENABLED=0 is fine because modernc sqlite is pure go (no C compiler needed).
# expects root ssh (hetzner default). idempotent, rerun after any change
deploy:
	@test -n "$(DEPLOY_HOST)" || (echo "set DEPLOY_HOST=user@host" && exit 1)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o dist/$(BINARY) ./cmd/crier
	ssh $(DEPLOY_HOST) 'id -u crier >/dev/null 2>&1 || useradd -r -s /usr/sbin/nologin crier; mkdir -p /opt/crier'
	scp dist/$(BINARY) $(DEPLOY_HOST):/usr/local/bin/$(BINARY)
	scp config.yaml config.local.yaml $(DEPLOY_HOST):/opt/crier/
	scp systemd/crier.service systemd/crier.timer $(DEPLOY_HOST):/etc/systemd/system/
	ssh $(DEPLOY_HOST) 'chown -R crier:crier /opt/crier && systemctl daemon-reload && systemctl enable --now crier.timer'

clean:
	rm -rf $(BINARY) dist/
