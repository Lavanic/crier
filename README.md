<p align="center">
  <img src="assets/town_crier.jpg" alt="a town crier ringing his bell" width="300">
</p>

A single-binary Go bot that watches 125 sources (123 Greenhouse/Lever/Ashby
company boards + 2 aggregator feeds) and fires an iOS Critical Alert on my phone within about a minute of a new-grad SWE role going live. Built for the December 2026 / June 2027 new-grad cycle, where some places routinely close postings within hours (sometimes with hard caps on application count). Being in the first 50 applicants is kinda the goal. No dashboard / web UI. I'm just getting paged like an on-call so it punches through DnD, and tapping it opens the apply page. The project as a whole was meant to see how much I could minimize the latency :)

```
systemd timer (every 30s on a $4 vps)
  └─ crier (one tick, then exit)
       ├─ fan out: greenhouse / lever / ashby boards + 2 aggregator feeds
       ├─ filter:  include regexes + exclude keywords (title only)
       ├─ dedup:   sqlite, INSERT OR IGNORE on {source}:{company}:{job_id}
       └─ notify:  pushover priority-2 emergency alert, retries until acked
```

## Why polling can be this fast

The public, unauthenticated board APIs (Greenhouse, Lever, Ashby) are heavily
cached and tolerate frequent reads. Polling them directly every 30s means
~60-90s worst-case from posting to phone buzz. The two GitHub aggregator
feeds ([SimplifyJobs/New-Grad-Positions](https://github.com/SimplifyJobs/New-Grad-Positions)
and [vanshb03/New-Grad-2027](https://github.com/vanshb03/New-Grad-2027),
credit where due, they do the heavy scraping for big tech) act as the dragnet
for companies not on the slug list, at their 5-30 minute refresh cadence.

## Quick start (local)

Needs Go 1.26+.

```sh
cp examples/config.yaml config.yaml   # slug list + filters, edit away
cat > config.local.yaml <<EOF         # gitignored creds
pushover:
  app_token: your-app-token
  user_key: your-user-key
EOF

make dry-run   # seeds the db, logs would-notify jobs, sends nothing
make test      # offline unit tests
make smoke     # live hits against real boards + one priority-0 test ping
```

Run `make dry-run` once before running for real: on a fresh database every
job counts as new, and you do not want ~500 emergency alerts.

## Deploy

Any Linux box with systemd works. I use a $4 DigitalOcean droplet in NYC.

```sh
make deploy DEPLOY_HOST=root@your.server.ip
```

That cross-compiles a static binary (pure-Go sqlite, no CGo), creates a
locked-down `crier` user, ships binary + configs + systemd units, and enables
the 30s timer. Verify with:

```sh
ssh root@your.server.ip
systemctl status crier.timer
journalctl -u crier -f        # structured json, one line per source per tick
```

On the phone: Pushover app → Settings → Notification Settings → Emergency
Priority → enable Critical Alerts.

## Config

Everything lives in `config.yaml` (committed) except credentials
(`config.local.yaml` or `CRIER_PUSHOVER_*` env vars). Adding a company is one
line, no rebuild:

```yaml
sources:
  greenhouse:
    - stripe        # https://boards-api.greenhouse.io/v1/boards/{slug}/jobs
  lever:
    - palantir      # https://api.lever.co/v0/postings/{slug}?mode=json
  ashby:
    - ramp          # https://api.ashbyhq.com/posting-api/job-board/{slug}
```

Find a company's slug by checking which of those three URLs answers with
jobs. Every slug shipped in `examples/config.yaml` was verified live; the
comments record the traps (DoorDash is `doordashusa`, Lever's `Coda` is
case-sensitive, plain `runway` is a different company, and so on).

Filters are title-only regexes, tuned recall-first: I would rather get pinged
twice than miss one posting. The `exclude_keywords` list (senior, intern,
recruiter, ...) matches whole words only, so "International" does not trip
"intern".

## Operational notes

- A source erroring or returning 0 jobs logs a WARN with the source name.
  One dead board never kills a tick.
- The db is append-only memory: jobs are marked seen *before* filtering, so
  loosening filters later never re-alerts old postings.
- Pushover free quota is 10k messages/month, pooled per account. A sane
  filter uses a few hundred.
- Deliberately not here (keeping v1 honest): LinkedIn/Indeed scraping,
  Workday tenants, headless browsers, LLM filtering, dashboards, Docker.

## License

MIT
