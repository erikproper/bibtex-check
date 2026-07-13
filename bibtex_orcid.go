/*
 *
 * Module:    bibtex_check_dev
 * Package:   Main
 * Component: ORCIDIntegration
 *
 * ORCID public API integration — connectivity check and person-record fetch.
 *
 * Creator: Henderik A. Proper (erikproper@gmail.com)
 *
 * Version of: 25.06.2026
 *
 */

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	orcidAPIBase    = "https://pub.orcid.org/v3.0"
	onlineProbeHost = "8.8.8.8:53" // Google public DNS — general connectivity probe
	dialTimeout     = 3 * time.Second
	httpTimeout     = 10 * time.Second
)

// Online is set once at startup: true when general internet connectivity is available.
var Online bool

func init() {
	_, err := net.DialTimeout("tcp", onlineProbeHost, dialTimeout)
	Online = err == nil
}

// orcidPersonResult holds name information extracted from an ORCID /person response.
type orcidPersonResult struct {
	CreditName   string   // preferred published name (natural order, may be empty)
	DeclaredName string   // "Family, Given" from the ORCID name record (may be empty)
	OtherNames   []string // additional name forms from other-names list
}

// orcidFetchStatus classifies the outcome of a fetchORCIDPerson call.
type orcidFetchStatus int

const (
	orcidFetchOK          orcidFetchStatus = iota // 200: result is valid
	orcidFetchNotFound                            // 404/410: profile absent or deactivated
	orcidFetchRateLimited                         // 429: back off and retry later
	orcidFetchError                               // other transient error
)

// fetchORCIDPersonWithStatus fetches the ORCID person record and returns both the
// parsed result and a status code so callers can distinguish permanent failures
// (not found) from transient ones (rate limit, network error).
func fetchORCIDPersonWithStatus(orcid string) (*orcidPersonResult, orcidFetchStatus) {
	url := fmt.Sprintf("%s/%s/person", orcidAPIBase, orcid)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, orcidFetchError
	}
	req.Header.Set("Accept", "application/json")
	client := &http.Client{Timeout: httpTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, orcidFetchError
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == 200:
		// handled below
	case resp.StatusCode == 429:
		return nil, orcidFetchRateLimited
	default:
		// 404, 410, 403 (private), 500, or any other non-200:
		// treat as "not fetchable" — save an empty marker so we stop retrying
		// until the one-month freshness window expires.
		return nil, orcidFetchNotFound
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, orcidFetchError
	}

	var p struct {
		Name struct {
			CreditName struct{ Value string `json:"value"` } `json:"credit-name"`
			FamilyName struct{ Value string `json:"value"` } `json:"family-name"`
			GivenNames struct{ Value string `json:"value"` } `json:"given-names"`
		} `json:"name"`
		OtherNames struct {
			OtherName []struct{ Content string `json:"content"` } `json:"other-name"`
		} `json:"other-names"`
	}
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, orcidFetchError
	}

	result := &orcidPersonResult{}
	result.CreditName = dblpPersonNameToLaTeX(strings.TrimSpace(p.Name.CreditName.Value))

	family := dblpPersonNameToLaTeX(strings.TrimSpace(p.Name.FamilyName.Value))
	given := dblpPersonNameToLaTeX(strings.TrimSpace(p.Name.GivenNames.Value))
	if family != "" && given != "" {
		result.DeclaredName = family + ", " + given
	} else if family != "" {
		result.DeclaredName = family
	}

	for _, n := range p.OtherNames.OtherName {
		if c := dblpPersonNameToLaTeX(strings.TrimSpace(n.Content)); c != "" {
			result.OtherNames = append(result.OtherNames, c)
		}
	}
	return result, orcidFetchOK
}

// fetchORCIDPerson is a convenience wrapper that discards the status code.
// Used by getORCIDPerson where status-aware handling is not needed.
func fetchORCIDPerson(orcid string) *orcidPersonResult {
	result, _ := fetchORCIDPersonWithStatus(orcid)
	return result
}

// orcidFolder returns the path to the ORCID disk cache directory (parallel to dblpFolder).
func orcidFolder() string {
	if globalFolder != "" {
		return globalFolder + "ORCID.cache/"
	}
	return bibTeXFolder + "ORCID.cache/"
}

// orcidCachePath returns the data.json path for a given ORCID.
// Layout: ORCID.cache/<first-4-digits>/<full-orcid>/data.json
func orcidCachePath(orcid string) string {
	if len(orcid) < 4 {
		return ""
	}
	return orcidFolder() + orcid[:4] + "/" + orcid + "/data.json"
}

// orcidCachedWork holds one ORCID work-group entry as stored in the disk cache.
// PutCode is the stable per-user identifier from the preferred work-summary.
type orcidCachedWork struct {
	PutCode int      `json:"put_code"`
	DOIs    []string `json:"dois,omitempty"`
}

// orcidCacheEntry is the on-disk JSON format for a cached ORCID record.
// Works and WorksFetchedAt are absent in old cache files; WorksFetchedAt.IsZero()
// signals that the works list has not yet been fetched for this entry.
type orcidCacheEntry struct {
	CreditName     string            `json:"credit_name"`
	DeclaredName   string            `json:"declared_name"`
	OtherNames     []string          `json:"other_names,omitempty"`
	FetchedAt      time.Time         `json:"fetched_at"`
	Works          []orcidCachedWork `json:"works,omitempty"`
	WorksFetchedAt time.Time         `json:"works_fetched_at,omitempty"`
}

// loadCachedORCIDPerson reads a cached person record from disk.
// Returns nil when the cache file is absent or unreadable.
func loadCachedORCIDPerson(orcid string) *orcidCacheEntry {
	path := orcidCachePath(orcid)
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var e orcidCacheEntry
	if err := json.Unmarshal(data, &e); err != nil {
		return nil
	}
	return &e
}

// saveCachedORCIDPerson writes a person record to the disk cache.
func saveCachedORCIDPerson(orcid string, result *orcidPersonResult) {
	path := orcidCachePath(orcid)
	if path == "" {
		return
	}
	dir := path[:strings.LastIndex(path, "/")]
	if err := os.MkdirAll(dir, 0755); err != nil {
		return
	}
	e := orcidCacheEntry{
		CreditName:   result.CreditName,
		DeclaredName: result.DeclaredName,
		OtherNames:   result.OtherNames,
		FetchedAt:    time.Now().UTC(),
	}
	data, err := json.Marshal(e)
	if err != nil {
		return
	}
	os.WriteFile(path, data, 0644) //nolint:errcheck
}

// saveCachedORCIDWorks updates the works fields in the cache entry for orcid.
// Loads the existing entry (preserving person data), sets Works and WorksFetchedAt,
// and writes back. Works may be nil or empty when the profile has no public works.
func saveCachedORCIDWorks(orcid string, works []orcidCachedWork) {
	path := orcidCachePath(orcid)
	if path == "" {
		return
	}
	var e orcidCacheEntry
	if data, err := os.ReadFile(path); err == nil {
		json.Unmarshal(data, &e) //nolint:errcheck
	}
	e.Works = works
	e.WorksFetchedAt = time.Now().UTC()
	data, err := json.Marshal(e)
	if err != nil {
		return
	}
	os.WriteFile(path, data, 0644) //nolint:errcheck
}

// getORCIDPerson returns the ORCID person record for orcid, using the disk cache
// when available and falling back to a live API fetch on a miss.
// The fetched result is written to the cache so subsequent calls are instant.
// Returns nil when the ORCID cannot be resolved (offline, not found, parse error).
func getORCIDPerson(orcid string) *orcidPersonResult {
	if e := loadCachedORCIDPerson(orcid); e != nil {
		return &orcidPersonResult{
			CreditName:   e.CreditName,
			DeclaredName: e.DeclaredName,
			OtherNames:   e.OtherNames,
		}
	}
	if !Online {
		return nil
	}
	result, status := fetchORCIDPersonWithStatus(orcid)
	switch status {
	case orcidFetchOK:
		saveCachedORCIDPerson(orcid, result)
		return result
	case orcidFetchNotFound:
		// Permanently absent — save empty markers for both person and works so future
		// runs skip both network calls without retrying known-broken profiles.
		saveCachedORCIDPerson(orcid, &orcidPersonResult{})
		saveCachedORCIDWorks(orcid, nil)
	}
	return nil
}

// getORCIDWorks returns the cached works list for orcid. When the works have not
// yet been fetched (WorksFetchedAt is zero) and we are online, it fetches from
// the network and updates the cache. Returns nil when offline or on fetch failure.
func getORCIDWorks(orcid string) []orcidCachedWork {
	if e := loadCachedORCIDPerson(orcid); e != nil && !e.WorksFetchedAt.IsZero() {
		return e.Works
	}
	if !Online {
		return nil
	}
	works, status := fetchORCIDWorks(orcid)
	if status == orcidFetchOK || status == orcidFetchNotFound {
		saveCachedORCIDWorks(orcid, works)
	}
	return works
}

// orcidCacheAge returns the FetchedAt time for a cached entry, or the zero time
// when no cache entry exists (making it sort as "oldest").
func orcidCacheAge(orcid string) time.Time {
	if e := loadCachedORCIDPerson(orcid); e != nil {
		return e.FetchedAt
	}
	return time.Time{}
}

// doUpdateOrcidCache refreshes the ORCID disk cache for all contributors that have
// an ORCID, working from the stalest entry (missing cache = oldest) toward the
// most recently cached. After the person pass, a works-backfill pass fetches works
// for entries whose person data is already fresh but whose works have never been
// cached. Stops cleanly when the user presses q+Enter.
func doUpdateOrcidCache() {
	// Re-probe connectivity now, not just at startup, since the network may have
	// changed state since the binary was launched.
	_, connErr := net.DialTimeout("tcp", onlineProbeHost, dialTimeout)
	if connErr != nil {
		fmt.Fprintf(os.Stderr, "No network connectivity — cannot refresh ORCID cache.\n")
		return
	}
	if !openLibraryToReport() {
		return
	}

	type orcidAge struct {
		orcid string
		age   time.Time
	}

	// Collect unique ORCIDs from contributors. Split into:
	//   entries     — person data stale (missing or older than one month)
	//   freshORCIDs — person data fresh but works may still be missing
	cutoff := time.Now().AddDate(0, -1, 0)
	seen := map[string]bool{}
	var entries []orcidAge
	var freshORCIDs []string
	total, fresh := 0, 0
	for _, contrib := range Library.ContributorByID {
		if contrib.ORCID == "" || seen[contrib.ORCID] {
			continue
		}
		seen[contrib.ORCID] = true
		total++
		age := orcidCacheAge(contrib.ORCID)
		if !age.IsZero() && age.After(cutoff) {
			fresh++
			freshORCIDs = append(freshORCIDs, contrib.ORCID)
			continue
		}
		entries = append(entries, orcidAge{orcid: contrib.ORCID, age: age})
	}

	// Works-backfill candidates: fresh person data but WorksFetchedAt is zero.
	var worksEntries []string
	for _, orcid := range freshORCIDs {
		if e := loadCachedORCIDPerson(orcid); e != nil && e.WorksFetchedAt.IsZero() {
			worksEntries = append(worksEntries, orcid)
		}
	}

	if len(entries) == 0 && len(worksEntries) == 0 {
		Library.Progress("ORCID cache up to date: all %d entries cached within the last month.", total)
		return
	}

	// Sort person entries: missing (zero time) first, then oldest-fetched first.
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].age.Before(entries[j].age)
	})

	missing := 0
	for _, e := range entries {
		if e.age.IsZero() {
			missing++
		}
	}
	stale := len(entries) - missing

	quitCh := make(chan struct{}, 1)
	stopCh := make(chan struct{})
	defer close(stopCh)
	go func() {
		for {
			select {
			case line, ok := <-stdinCh:
				if !ok || strings.TrimSpace(line) == "q" {
					select {
					case quitCh <- struct{}{}:
					default:
					}
					return
				}
			case <-stopCh:
				return
			}
		}
	}()

	// Person pass — also fetches works for each entry in the same iteration.
	if len(entries) > 0 {
		Library.Progress("Refreshing ORCID cache: %d person(s) to fetch (%d missing, %d stale); %d fresh, skipped. q+Enter to stop.",
			len(entries), missing, stale, fresh)
		ticker := Library.NewProgressTicker("Refreshing ORCID cache", len(entries))
		fetched, failed, consecutiveFails := 0, 0, 0
		for i, e := range entries {
			select {
			case <-quitCh:
				ticker.Done()
				Library.Progress("ORCID cache refresh stopped after %d/%d; %d fetched, %d failed.",
					i, len(entries), fetched, failed)
				return
			default:
			}
			ticker.Step()
			result, status := fetchORCIDPersonWithStatus(e.orcid)
			switch status {
			case orcidFetchOK:
				consecutiveFails = 0
				saveCachedORCIDPerson(e.orcid, result)
				if works, wStatus := fetchORCIDWorks(e.orcid); wStatus == orcidFetchOK || wStatus == orcidFetchNotFound {
					saveCachedORCIDWorks(e.orcid, works)
				}
				fetched++
				time.Sleep(500 * time.Millisecond)
			case orcidFetchNotFound:
				// Profile absent or deactivated — save empty markers so this ORCID is
				// treated as "fresh" in future runs and not retried unnecessarily.
				saveCachedORCIDPerson(e.orcid, &orcidPersonResult{})
				saveCachedORCIDWorks(e.orcid, nil)
				failed++
			case orcidFetchRateLimited:
				failed++
				consecutiveFails++
				if consecutiveFails >= 5 {
					ticker.Done()
					Library.Progress("ORCID cache refresh paused: rate-limited after %d attempts. "+
						"Re-run later; %d cached so far.", consecutiveFails, fetched)
					return
				}
				time.Sleep(5 * time.Second)
			default: // orcidFetchError
				failed++
				consecutiveFails++
				if consecutiveFails >= 5 {
					ticker.Done()
					Library.Progress("ORCID cache refresh paused after %d consecutive errors. "+
						"Re-run later; %d cached so far.", consecutiveFails, fetched)
					return
				}
				time.Sleep(2 * time.Second)
			}
		}
		ticker.Done()
		Library.Progress("ORCID cache refresh complete: %d fetched, %d failed.", fetched, failed)
	}

	// Works-backfill pass — one API call per entry; person data already fresh.
	if len(worksEntries) > 0 {
		Library.Progress("Backfilling ORCID works cache: %d entries missing works data. q+Enter to stop.", len(worksEntries))
		ticker := Library.NewProgressTicker("Backfilling works cache", len(worksEntries))
		fetched, failed := 0, 0
		for i, orcid := range worksEntries {
			select {
			case <-quitCh:
				ticker.Done()
				Library.Progress("Works backfill stopped after %d/%d; %d fetched, %d failed.",
					i, len(worksEntries), fetched, failed)
				return
			default:
			}
			ticker.Step()
			works, wStatus := fetchORCIDWorks(orcid)
			switch wStatus {
			case orcidFetchOK, orcidFetchNotFound:
				saveCachedORCIDWorks(orcid, works)
				fetched++
			default:
				// Transient or unrecognised error — mark done with nil works so the
				// backfill does not retry indefinitely. The monthly person-refresh pass
				// will re-populate works when person data next becomes stale.
				saveCachedORCIDWorks(orcid, nil)
				failed++
			}
			time.Sleep(500 * time.Millisecond)
		}
		ticker.Done()
		Library.Progress("Works backfill complete: %d fetched, %d failed.", fetched, failed)
	}
}

// fetchORCIDWorks fetches the public ORCID works list for a given ORCID.
// Returns (works, orcidFetchOK) on a 200 response — works may be empty when the
// profile has no public works. Returns orcidFetchNotFound for 404/410/403,
// orcidFetchRateLimited for 429, and orcidFetchError for transient failures.
func fetchORCIDWorks(orcid string) ([]orcidCachedWork, orcidFetchStatus) {
	url := fmt.Sprintf("%s/%s/works", orcidAPIBase, orcid)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, orcidFetchError
	}
	req.Header.Set("Accept", "application/json")
	client := &http.Client{Timeout: httpTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, orcidFetchError
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == 200:
		// handled below
	case resp.StatusCode == 429:
		return nil, orcidFetchRateLimited
	case resp.StatusCode == 404 || resp.StatusCode == 410 || resp.StatusCode == 403:
		return nil, orcidFetchNotFound
	default:
		return nil, orcidFetchError
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, orcidFetchError
	}

	var raw struct {
		Group []struct {
			ExternalIDs struct {
				ExternalID []struct {
					Type  string `json:"external-id-type"`
					Value string `json:"external-id-value"`
				} `json:"external-id"`
			} `json:"external-ids"`
			WorkSummary []struct {
				PutCode int `json:"put-code"`
			} `json:"work-summary"`
		} `json:"group"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, orcidFetchError
	}

	var result []orcidCachedWork
	for _, grp := range raw.Group {
		if len(grp.WorkSummary) == 0 || grp.WorkSummary[0].PutCode == 0 {
			continue
		}
		putCode := grp.WorkSummary[0].PutCode
		seen := map[string]bool{}
		var dois []string
		for _, id := range grp.ExternalIDs.ExternalID {
			if id.Type == "doi" {
				doi := strings.TrimPrefix(strings.TrimPrefix(id.Value, "https://doi.org/"), "http://doi.org/")
				doi = strings.ToLower(strings.TrimSpace(doi))
				if doi != "" && !seen[doi] {
					seen[doi] = true
					dois = append(dois, doi)
				}
			}
		}
		result = append(result, orcidCachedWork{PutCode: putCode, DOIs: dois})
	}
	return result, orcidFetchOK
}

// doiBareFieldRE matches a BibTeX field value that is a bare alphabetic word
// (a string-macro reference) rather than a braced or quoted literal — e.g.
// month=Nov, or month=July }. doi.org (CrossRef) emits month this way; our
// parser does not support undefined string macros, so we wrap them in braces.
var doiBareFieldRE = regexp.MustCompile(`(=\s*)([A-Za-z][A-Za-z]*)(\s*[,}])`)

// sanitizeDoiBibTeX fixes known quirks in BibTeX returned by doi.org:
//   - bare alphabetic field values (string macro refs) are wrapped in braces
func sanitizeDoiBibTeX(raw string) string {
	return doiBareFieldRE.ReplaceAllString(raw, "${1}{${2}}${3}")
}

// fetchDoiBibTeX fetches the BibTeX record for a DOI from doi.org using content
// negotiation. Returns the sanitized BibTeX string or "" on any error.
func fetchDoiBibTeX(doi string) string {
	req, err := http.NewRequest("GET", "https://doi.org/"+doi, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("Accept", "application/x-bibtex")
	client := &http.Client{Timeout: httpTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return ""
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return ""
	}
	return sanitizeDoiBibTeX(strings.TrimSpace(string(body)))
}

// uniqueForms returns the unique non-empty strings from the input list, preserving order.
func uniqueForms(forms ...string) []string {
	seen := map[string]bool{}
	var out []string
	for _, f := range forms {
		if f != "" && !seen[f] {
			seen[f] = true
			out = append(out, f)
		}
	}
	return out
}

// declaredNameHasDifferentSurname reports whether declaredName (Last,First from ORCID
// family-name + given-names) and currentName are both in Last,First BibTeX format but
// have different surnames. This signals a compound-surname boundary error in the current
// canonical: even when the credit-name matches by form, the declared name should still
// be offered as a correction option.
func declaredNameHasDifferentSurname(currentName, declaredName string) bool {
	if declaredName == "" || currentName == declaredName {
		return false
	}
	currentParts := strings.SplitN(currentName, ", ", 2)
	declaredParts := strings.SplitN(declaredName, ", ", 2)
	if len(currentParts) != 2 || len(declaredParts) != 2 {
		return false
	}
	cs := strings.ToLower(currentParts[0])
	ds := strings.ToLower(declaredParts[0])
	// Same after case folding (handles ALL-CAPS ORCID entries like "MARIANI, Joseph").
	if cs == ds {
		return false
	}
	// One surname contains the other: compound/patronymic surnames, "van der" prefixes,
	// hyphenated names, and married-name additions (e.g. "née").
	if strings.Contains(cs, ds) || strings.Contains(ds, cs) {
		return false
	}
	return true
}

// inferBibTeXFromCreditName tries to reconstruct a BibTeX Last,First canonical from
// a natural-order credit-name, using the surname known from the declared name.
// When the credit-name ends with the declared surname, the prefix becomes the given
// names and the result is "Surname, GivenNames".
// Returns "" when the inference is not possible (no surname match or ambiguous split).
func inferBibTeXFromCreditName(creditName, declaredName string) string {
	if creditName == "" || declaredName == "" {
		return ""
	}
	parts := strings.SplitN(declaredName, ", ", 2)
	if len(parts) != 2 {
		return ""
	}
	surname := parts[0]
	if !strings.HasSuffix(creditName, " "+surname) {
		return ""
	}
	given := strings.TrimSpace(strings.TrimSuffix(creditName, " "+surname))
	if given == "" {
		return ""
	}
	return surname + ", " + given
}

// creditNameMatches reports whether currentName is considered a match for creditName:
// either an exact "as-is" match, or a match after converting currentName from
// Last,First BibTeX order to natural First Last order (or vice versa).
// Returns false when creditName is empty.
func creditNameMatches(currentName, creditName string) bool {
	if creditName == "" || currentName == "" {
		return false
	}
	if currentName == creditName {
		return true
	}
	// currentName in Last,First → swap to First Last and compare
	if swapBibTeXNameFormat(currentName) == creditName {
		return true
	}
	// creditName in Last,First → swap to First Last and compare
	if swapBibTeXNameFormat(creditName) == currentName {
		return true
	}
	return false
}
