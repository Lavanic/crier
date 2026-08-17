<p align="center">
  <img src="assets/town_crier.jpg" alt="a town crier ringing his bell" width="300">
</p>

<p align="center">
  <img src="https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white" alt="Go 1.26">
  <img src="https://img.shields.io/badge/platform-Linux%20%2F%20systemd-333?logo=linux&logoColor=white" alt="Linux / systemd">
  <img src="https://img.shields.io/badge/binary-single%2C%20no%20CGO-3fb950" alt="single binary, no CGO">
  <img src="https://img.shields.io/badge/license-MIT-yellow" alt="License: MIT">
</p>

A single-binary Go bot that watches 135 sources (128 Greenhouse/Lever/Ashby
company boards, 2 aggregator feeds, plus the Google, Apple, Netflix and NVIDIA
careers sites directly) and fires an iOS Critical Alert on my phone within about a minute of a new-grad SWE role going live. Built for the December 2026 / June 2027 new-grad cycle, where some places routinely close postings within hours (sometimes with hard caps on application count). Being in the first 50 applicants is kinda the goal. No dashboard / web UI. Postings from a hand-picked list of priority companies page me like an on-call, punching through DnD; everything else arrives as a normal ping. Tapping either opens the apply page. The project as a whole was meant to see how much I could minimize the latency :)

```
systemd timer (every 30s on a $4 vps)
  └─ crier (one tick, then exit)
       ├─ fan out: greenhouse / lever / ashby boards + aggregator feeds
       │           + google / apple / netflix / nvidia careers pages
       ├─ filter:  include regexes + exclude keywords/patterns,
       │           us-only location gate, feed category gate
       ├─ dedup:   sqlite INSERT OR IGNORE on {source}:{company}:{job_id},
       │           plus cross-portal dedup on {company}:{req id}
       └─ notify:  pushover. priority_companies siren (emergency,
                   re-buzzes until acked), the rest ping normally
```

## Why polling can be this fast

Speed speed speed speed. The public, unauthenticated board APIs (Greenhouse,
Lever, Ashby) are heavily cached and shrug off frequent reads, so crier hits
them directly every 30 seconds. **From a recruiter hitting publish to a buzz in my pocket
in ~60 seconds worst case**, usually less. Other folks refresh their slow
aggregators or wait on email digests and apply the next time they check their inbox;
polling the source itself collapses that to seconds, which is the difference
between applicant #6 and applicant #600 when a role caps out the same afternoon
it opens. The two GitHub aggregator feeds ([SimplifyJobs/New-Grad-Positions](https://github.com/SimplifyJobs/New-Grad-Positions)
and [vanshb03/New-Grad-2027](https://github.com/vanshb03/New-Grad-2027),
credit where due, they do heavy scraping for big tech) still ride along as
a dragnet for companies not on the slug list, at their slower 5-30 minute
refresh cadence.

Google, Apple, Netflix and NVIDIA run their own career sites with no public
board API, so crier scrapes each directly, polling a date-sorted,
server-side-filtered slice every 5 minutes with no headless browser needed.

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

The include regexes are tuned recall-first (wide net), and the exclude
layers do the precision work: `exclude_keywords` (senior, intern, field
service, propulsion, ...) matches whole words only, so "International" does
not trip "intern"; `exclude_patterns` are raw regexes checked against the
URL too, because Workday slugs leak start dates titles hide;
`exclude_locations` drops a posting only when *every* listed location is
non-US; and `exclude_categories` drops the aggregator feed's Hardware and
Product listings unless the title sounds like software anyway. The whole
stack is regression-tested against a hand-labeled corpus of real alerts
(`cmd/crier/realdata_test.go`), so tuning a filter immediately shows which
real postings flip.

## Operational notes

- A source erroring or returning 0 jobs logs a WARN with the source name.
  One dead board never kills a tick.
- `min_interval_seconds` is a floor whether the fetch succeeded or not, so a
  failing source backs off instead of retrying every 30s.
- A source with no successful fetch in 6h sends one Pushover, then goes quiet
  for 24h. The dead-man switch only catches a dead *tick*, and one dead source
  leaves it green, so this is the only thing that notices a board going away.
- The db is append-only memory: jobs are marked seen *before* filtering, so
  loosening filters later never re-alerts old postings.
- Pushover free quota is 10k messages/month, pooled per account. A sane
  filter uses a few hundred.

## License

MIT
