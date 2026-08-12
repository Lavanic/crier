package sources

import (
	"net/url"
	"strings"
	"unicode"
)

// a story link sticker is a url and nothing else, so company, title
// and location all have to come out of the url. pure functions only,
// the network side is in instagram.go

// instagram hides the real destination behind its own click tracker
var linkShims = map[string]bool{
	"l.instagram.com": true,
	"l.facebook.com":  true,
	"lm.facebook.com": true,
	"l.messenger.com": true,
	"href.li":         true,
}

// his linktree and the occasional reel share, not job postings
var notJobHosts = map[string]bool{
	"instagram.com":     true,
	"www.instagram.com": true,
	"youtube.com":       true,
	"www.youtube.com":   true,
	"youtu.be":          true,
	"tiktok.com":        true,
	"www.tiktok.com":    true,
	"twitter.com":       true,
	"x.com":             true,
	"discord.gg":        true,
	"discord.com":       true,
	"linktr.ee":         true,
	"t.me":              true,
	"open.spotify.com":  true,
	"chat.whatsapp.com": true,
}

// analytics junk. short on purpose, stripping a param that names the
// posting breaks the apply link, way worse than a duplicate alert
var trackingParams = map[string]bool{
	"fbclid":  true,
	"igshid":  true,
	"igsh":    true,
	"gclid":   true,
	"msclkid": true,
	"mc_cid":  true,
	"mc_eid":  true,
	"_hsenc":  true,
	"_hsmi":   true,
	"gh_src":  true,
	"trk":     true,
	"ref_src": true,
}

// loops because facebook sometimes wraps an already wrapped link
func unwrapStoryLink(raw string) string {
	for range 3 {
		u, err := url.Parse(raw)
		if err != nil || !linkShims[strings.ToLower(u.Hostname())] {
			return raw
		}
		// Get already url-decodes, so one hop is one decode
		target := u.Query().Get("u")
		if target == "" {
			return raw
		}
		raw = target
	}
	return raw
}

// this is the dedup id, so it has to come out identical when he
// reposts the same job with fresh utm tags. hence sorting the params
func canonicalJobURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Hostname() == "" {
		return strings.TrimSpace(raw)
	}
	q := u.Query()
	for k := range q {
		lk := strings.ToLower(k)
		if strings.HasPrefix(lk, "utm_") || trackingParams[lk] {
			q.Del(k)
		}
	}
	u.RawQuery = q.Encode()
	u.Fragment = ""
	u.Host = strings.ToLower(u.Host)
	if u.Scheme == "" {
		u.Scheme = "https"
	}
	if u.Path != "/" {
		u.Path = strings.TrimRight(u.Path, "/")
	}
	return u.String()
}

// host labels that name the recruiting site, not the company.
// "us" and "en" for locale-fronted boards like us.mercer.com
var hostNoise = map[string]bool{
	"www": true, "careers": true, "career": true, "jobs": true,
	"job": true, "apply": true, "boards": true, "board": true,
	"recruiting": true, "recruit": true, "hire": true, "hiring": true,
	"talent": true, "work": true, "join": true, "us": true, "en": true,
}

// board slug + posting id off the four ats hosts crier already
// speaks. greenhouse.go and friends use the slug as Company too, so a
// link to a board we poll lines up and cross-post dedup kills it
func boardSlug(u *url.URL) (kind, slug, jobID string) {
	host := strings.ToLower(u.Hostname())
	segs := pathSegments(u)

	switch {
	case strings.HasSuffix(host, "greenhouse.io"):
		// the embed form carries the slug in a query param instead
		if len(segs) > 0 && segs[0] == "embed" {
			return "greenhouse", u.Query().Get("for"), u.Query().Get("token")
		}
		if len(segs) >= 3 && segs[1] == "jobs" {
			return "greenhouse", segs[0], segs[2]
		}
	case strings.HasSuffix(host, "lever.co"):
		if len(segs) >= 2 {
			return "lever", segs[0], segs[1]
		}
	case strings.HasSuffix(host, "ashbyhq.com"):
		if len(segs) >= 2 {
			return "ashby", segs[0], segs[1]
		}
	case strings.HasSuffix(host, "myworkdayjobs.com"):
		// tenant is the first host label (tamus.wd1.myworkdayjobs.com)
		if i := strings.IndexByte(host, '.'); i > 0 {
			return "workday", host[:i], ""
		}
	}
	return "", "", ""
}

// board slug if we know the host, else the first real domain label
// (careers.peak6.com -> peak6)
func companyFromURL(u *url.URL) string {
	if _, slug, _ := boardSlug(u); slug != "" {
		return slug
	}
	for _, label := range strings.Split(strings.ToLower(u.Hostname()), ".") {
		if label != "" && !hostNoise[label] {
			return label
		}
	}
	return strings.ToLower(u.Hostname())
}

func pathSegments(u *url.URL) []string {
	var out []string
	for _, s := range strings.Split(u.EscapedPath(), "/") {
		if s == "" {
			continue
		}
		if dec, err := url.PathUnescape(s); err == nil {
			s = dec
		}
		out = append(out, s)
	}
	return out
}

// boards use either separator, workday uses both
func slugTokens(seg string) []string {
	return strings.FieldsFunc(seg, func(r rune) bool {
		return r == '-' || r == '_' || r == '+' || r == ' '
	})
}

// real words in a slug. JR105057 and uuids score 0, which skips them
// without needing a pattern per board id format
func alphaTokens(seg string) int {
	n := 0
	for _, t := range slugTokens(seg) {
		if len(t) < 3 {
			continue
		}
		allAlpha := true
		for _, r := range t {
			if !unicode.IsLetter(r) {
				allAlpha = false
				break
			}
		}
		if allAlpha {
			n++
		}
	}
	return n
}

// right to left on purpose: boards put the title last and the office
// right before it, so "most words wins" would pick peak6's
// chicago-illinois-united-states-of-america over the actual role
func titleFromURL(u *url.URL) string {
	segs := pathSegments(u)
	for i := len(segs) - 1; i >= 0; i-- {
		seg := trimReqIDSuffix(segs[i])
		if alphaTokens(seg) < 2 || looksLikeLocation(seg) {
			continue
		}
		return prettifySlug(seg)
	}
	return ""
}

// only speaks up when a segment is clearly a place. the location gate
// fails open on empty, so silence is safe and a bad guess is not
func locationFromURL(u *url.URL) string {
	segs := pathSegments(u)
	for i := len(segs) - 1; i >= 0; i-- {
		if looksLikeLocation(segs[i]) {
			return prettifySlug(segs[i])
		}
	}
	return ""
}

// boards staple the req id to the title, workday with an underscore
// (...-Assistant_R-089229), others with a dash (...-new-grad-235049).
// lop it off so the lock screen isn't showing a req number
func trimReqIDSuffix(seg string) string {
	if i := strings.IndexByte(seg, '_'); i > 0 {
		head, tail := seg[:i], seg[i+1:]
		if alphaTokens(head) >= 2 && alphaTokens(tail) < 2 {
			seg = head
		}
	}
	// 5+ digits so a "2027" cohort tag survives, exclude_patterns
	// reads the wrong-year ones straight off the url
	if i := strings.LastIndexAny(seg, "-_"); i > 0 {
		if tail := seg[i+1:]; len(tail) >= 5 && isDigits(tail) {
			seg = seg[:i]
		}
	}
	return seg
}

func isDigits(s string) bool {
	for _, r := range s {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return s != ""
}

func looksLikeLocation(seg string) bool {
	tokens := slugTokens(strings.ToLower(seg))
	if len(tokens) == 0 {
		return false
	}
	joined := strings.Join(tokens, " ")
	if strings.Contains(joined, "united states") || strings.Contains(joined, "of america") {
		return true
	}
	if tokens[0] == "remote" {
		return true
	}
	// abbrevs only count last, "in", "or", "me" and "de" are all real
	// words that turn up mid-title
	if stateAbbrev[tokens[len(tokens)-1]] {
		return true
	}
	// a spelled-out state only decides a short segment, a long one is
	// more likely a title that happens to name a state
	if len(tokens) <= 3 {
		for _, t := range tokens {
			if stateName[t] {
				return true
			}
		}
	}
	return false
}

// slug to something readable on a lock screen. words that already
// carry caps (RSSI, AI) keep them
func prettifySlug(seg string) string {
	tokens := slugTokens(seg)
	out := make([]string, 0, len(tokens))
	for _, t := range tokens {
		if t == strings.ToLower(t) {
			r := []rune(t)
			r[0] = unicode.ToUpper(r[0])
			t = string(r)
		}
		out = append(out, t)
	}
	return strings.Join(out, " ")
}

// the whole pipeline for one link sticker. ok is false when the link
// isn't a posting at all
func jobFromStoryLink(sourceName, raw string) (Job, bool) {
	target := canonicalJobURL(unwrapStoryLink(raw))
	u, err := url.Parse(target)
	if err != nil || u.Hostname() == "" {
		return Job{}, false
	}
	if notJobHosts[strings.ToLower(u.Hostname())] {
		return Job{}, false
	}
	// a bare domain can't be a specific posting
	if len(pathSegments(u)) == 0 {
		return Job{}, false
	}
	return Job{
		Source:  "instagram:" + sourceName,
		Company: companyFromURL(u),
		// url as the id, not the sticker id: a repost is a new sticker
		JobID:    target,
		Title:    titleFromURL(u),
		Location: locationFromURL(u),
		URL:      target,
	}, true
}

var stateAbbrev = map[string]bool{
	"al": true, "ak": true, "az": true, "ar": true, "ca": true,
	"co": true, "ct": true, "dc": true, "de": true, "fl": true,
	"ga": true, "hi": true, "ia": true, "id": true, "il": true,
	"in": true, "ks": true, "ky": true, "la": true, "ma": true,
	"md": true, "me": true, "mi": true, "mn": true, "mo": true,
	"ms": true, "mt": true, "nc": true, "nd": true, "ne": true,
	"nh": true, "nj": true, "nm": true, "nv": true, "ny": true,
	"oh": true, "ok": true, "or": true, "pa": true, "ri": true,
	"sc": true, "sd": true, "tn": true, "tx": true, "ut": true,
	"va": true, "vt": true, "wa": true, "wi": true, "wv": true,
	"wy": true,
}

var stateName = map[string]bool{
	"alabama": true, "alaska": true, "arizona": true, "arkansas": true,
	"california": true, "colorado": true, "connecticut": true,
	"delaware": true, "florida": true, "georgia": true, "hawaii": true,
	"idaho": true, "illinois": true, "indiana": true, "iowa": true,
	"kansas": true, "kentucky": true, "louisiana": true, "maine": true,
	"maryland": true, "massachusetts": true, "michigan": true,
	"minnesota": true, "mississippi": true, "missouri": true,
	"montana": true, "nebraska": true, "nevada": true, "ohio": true,
	"oklahoma": true, "oregon": true, "pennsylvania": true,
	"tennessee": true, "texas": true, "utah": true, "vermont": true,
	"virginia": true, "washington": true, "wisconsin": true,
	"wyoming": true,
}
